package cmd

import (
	"context"
	"fmt"
	"github.com/crunchydata/postgres-operator-client/internal"
	"github.com/crunchydata/postgres-operator-client/internal/apis/postgres-operator.crunchydata.com/v1beta1"
	"github.com/crunchydata/postgres-operator-client/internal/display"
	"github.com/crunchydata/postgres-operator-client/internal/processing"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"regexp"
)

// holds value option repoName passed on the command line. This option determines
// from which repo we will create the clone.
var fromRepo string
var toNamespace string
var showYamlOfClone bool
var overrideConfigMapsAndSecrets bool
var pitr string
var lastBackup bool

// newBackupCommand returns the backup command of the PGO plugin.
// It optionally takes a `repoName` and `options` flag, which it uses
// to update the spec.
func newCloneCommand(config *internal.Config) *cobra.Command {

	// cmd represents the clone command
	var cmd = &cobra.Command{
		Use:   "clone",
		Short: "Creates a clone of a running crunchy cluster in the same namespace",
		Long: `Creates a clone from a given cluster into the same namespace.
- The clone is created from the latest data in the source cluster
- The clone is created with a local storage
- The clone uses the same postgres parameters as the source cluster
- The clone is named clone-<cluster-name> with a limit o X characters. If the 
  size exceeds the allowed size (Y), the latest letters are truncated. If there
  is a cluster name as the clone-<cluster-name>, the command fails.

#### RBAC Requirements
    Resources                                           Verbs
    ---------                                           -----
    configmap                                           [get create delete]
    namespace                                           [get create]
    pod                                                 [exec]
    postgresclusters.postgres-operator.crunchydata.com  [get create]
    secrets                                             [get create delete]
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			re := regexp.MustCompile("^repo[1-4]")
			if !re.MatchString(fromRepo) {
				return fmt.Errorf("the repoName option must be specified and the allowed values are repo[1-4]. Other values are rejected")
			}

			if pitr != "" && !processing.IsPitrSyntacticallyValid(pitr) {
				return fmt.Errorf("the expected format for the PITR is '2022-12-28 15:47:38+01'. You supplied %q", pitr)
			}

			clientK8s, restConfig, err := processing.ConfigureK8sClient(config.ConfigFlags)
			if err != nil {
				return err
			}

			clusterName := args[0]
			_, clientCrunchy, err := v1beta1.NewPostgresClusterClient(config)
			if err != nil {
				return err
			}
			namespace := *config.ConfigFlags.Namespace
			clusterToClone, err := clientCrunchy.Namespace(namespace).Get(context.TODO(), clusterName, metav1.GetOptions{})
			if err != nil {
				return errors.Wrapf(err, "failed to get cluster %q in namespace %q", clusterName, namespace)
			}

			if pitr != "" {
				err = processing.IsValidPitr(restConfig, namespace, clusterToClone, fromRepo, pitr)
				if err != nil {
					return errors.Wrap(err, "PITR is not valid")
				}
			}

			targetNamespace := namespace
			if toNamespace != "" {
				targetNamespace = toNamespace
			}

			clone, err := processing.GenerateCloneDefinitionWithLocalStorageFrom(clusterToClone, fromRepo, targetNamespace, pitr, lastBackup)
			if err != nil {
				return errors.Wrap(err, "failed to generate definition of clone")
			}

			// Add network policies if there are some in the namespace that targets the database
			err = processing.AddNetworkPoliciesIfRequired(clientK8s, restConfig, clusterToClone)
			if err != nil {
				return fmt.Errorf("failed to add network policies %w", err)
			}
			// postgres-operator.crunchydata.com/cluster=clone-cedito-jahiapg

			var additionalConfigPgBackrest v1.ConfigMap
			if !processing.HasPgbackrestAdditionalConfig(clone) {
				fmt.Println("Clone has no PGBackrest additional configuration. Adding one to make sure the restore trace is verbose and comprehensive in case of error")
				additionalConfigPgBackrest = processing.GenerateVerboseConfigForPgBackrest()
				additionalConfigPgBackrest.ObjectMeta.Name = fmt.Sprintf("%s-%s", processing.GenerateCloneName(clusterToClone.GetName()), additionalConfigPgBackrest.ObjectMeta.Name)
				processing.AddPgBackrestAdditionalConfigurationToClone(clone, additionalConfigPgBackrest)
			}

			// this might be requested by the user to check the clone's YAML definition
			if showYamlOfClone {
				content, err := yaml.Marshal(clone)
				if err != nil {
					return errors.Wrap(err, "failed to generate yaml for clone")
				}
				fmt.Printf("YAML of clone:\n%s\n", string(content))
			}

			// we have to know which resources we have created, so we can delete
			// them in case of failure
			var configMapsToDelete, secretsToDelete []string
			if targetNamespace != clusterToClone.GetNamespace() {
				err = processing.CreateNamespaceIfNotExists(clientK8s, targetNamespace)
				if err != nil {
					return errors.Wrapf(err, "failed to create namespace %q", namespace)
				}
				// keep track of resources created. The function might need to
				// delete them if the creation of the clone fails
				configMapsToDelete, secretsToDelete, err = processing.DumpConfigMapsAndSecretsIfNeeded(clientK8s, clusterToClone, targetNamespace, overrideConfigMapsAndSecrets)
				if err != nil {
					return errors.Wrap(err, "failed to dump objects before clone creation")
				}
			}
			if additionalConfigPgBackrest.Name != "" {
				fmt.Printf("Creating the additional configmap for pgbackrest with name %s...\n", additionalConfigPgBackrest.Name)
				cm, err := clientK8s.CoreV1().ConfigMaps(clone.GetNamespace()).Create(context.TODO(), &additionalConfigPgBackrest, metav1.CreateOptions{})
				if err != nil {
					return errors.Wrap(err, "failed to create configmap for pgbackrest configuration before clone creation")
				}
				configMapsToDelete = append(configMapsToDelete, cm.Name)
			} else {
				fmt.Println("Not creating additional configmap for pgbackrest...")
			}

			// Create the clone
			_, err = clientCrunchy.Namespace(targetNamespace).Create(context.TODO(), clone, metav1.CreateOptions{})
			if err != nil {
				if targetNamespace != clusterToClone.GetNamespace() {
					// leave the space clean : delete objects created previously
					display.ReportFailure(fmt.Sprintf("creation of clone failed. Since target namespace is different from source namespace, deletion of dumped resources to leave the space clean"), err)
					processing.DeleteConfigMaps(clientK8s, configMapsToDelete, targetNamespace)
					processing.DeleteSecrets(clientK8s, secretsToDelete, targetNamespace)
					processing.DeleteNetworkPoliciesIfRequired(clientK8s, restConfig, targetNamespace)
				}

				return errors.Wrapf(err, "failed to clone cluster %s/%s", clusterToClone.GetNamespace(), clusterToClone.GetName())
			}
			display.ReportSuccess(fmt.Sprintf("Clone of cluster %q successfully created as %q", clusterName, clone.GetName()))

			return nil
		},
	}
	cmd.Args = cobra.ExactArgs(1)
	cmd.Flags().StringVar(&fromRepo, "repoName", "", "repo name to clone from (repo1, repo2, etc)")
	cmd.Flags().StringVar(&toNamespace, "to-ns", "", "the target namespace where the clone will live")
	cmd.Flags().BoolVarP(&showYamlOfClone, "show-yaml", "", false, "request to show the yaml generated for the definition of the clone")
	cmd.Flags().BoolVarP(&overrideConfigMapsAndSecrets, "overrides-configs", "", false, "request to override configmaps and secrets if they already exist")
	cmd.Flags().StringVarP(&pitr, "pitr", "", "", "the point in time at which you want the clone to be restored to. Format is '2022-12-28 15:47:38+01'")
	cmd.Flags().BoolVar(&lastBackup, "last-backup", false, "Requires to use the last backup. The command will compute which backup is the last one and choose it.")

	return cmd
}
