/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"
	"github.com/crunchydata/postgres-operator-client/internal"
	"github.com/crunchydata/postgres-operator-client/internal/apis/postgres-operator.crunchydata.com/v1beta1"
	"github.com/fatih/color"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"os"
	"regexp"
	"strings"
)

var formatterOk = color.New(color.FgHiGreen)
var formatterNok = color.New(color.FgHiRed)

// holds value option repoName passed on the command line. This option determines
// from which repo we will create the clone.
var fromRepo string
var toNamespace string
var showYamlOfClone bool
var overrideConfigMapsAndSecrets bool

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
    postgresclusters.postgres-operator.crunchydata.com  [get create]
    namespace                                           [get create]
    configmap                                           [get create delete]
    secrets                                             [get create delete]
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			re := regexp.MustCompile("^repo[1-4]")
			if !re.MatchString(fromRepo) {
				return fmt.Errorf("the repoName option must be specified and the allowed values are repo[1-4]. Other values are rejected")
			}

			clientK8s := configureK8sClient(config.ConfigFlags)

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

			clone, err := generateCloneWithLocalStorageFrom(clusterToClone, fromRepo, toNamespace)
			if err != nil {
				return errors.Wrap(err, "failed to generate definition of clone")
			}
			// if changing of namespace, we need to dump potential configurations

			if showYamlOfClone {
				content, err := yaml.Marshal(clone)
				if err != nil {
					return errors.Wrap(err, "failed to generate yaml for clone")
				}
				fmt.Printf("YAML of clone:\n%s\n", string(content))
			}

			var configMapsToDump, secretsToDump []string
			// in case of failure, we have to know the created resources that we
			// have to delete
			var configMapsToDelete, secretsToDelete []string
			if toNamespace != "" && toNamespace != clusterToClone.GetNamespace() {
				err = createNamespaceIfNotExists(clientK8s, toNamespace)
				if err != nil {
					return errors.Wrapf(err, "failed to create namespace %q", namespace)
				}
				// find out if there are configmaps or secrets to be dumped
				configMapsToDump, secretsToDump = requiredConfigMapsAndSecretsFor(clusterToClone)
				for _, cm := range configMapsToDump {
					err = dumpConfigMapToNamespace(clientK8s, cm, clusterToClone.GetNamespace(), toNamespace, overrideConfigMapsAndSecrets)
					if err == nil {
						reportSuccess(fmt.Sprintf("Dump configmap %q to %q", cm, toNamespace))
						configMapsToDelete = append(configMapsToDelete, cm)
					} else {
						reportFailure(fmt.Sprintf("Dump configmap %q to %q", cm, toNamespace), err)
						fmt.Printf("Deleting resources previously created.")
						// try to delete them and return an error
						deleteConfigMaps(clientK8s, configMapsToDelete, toNamespace)
						return fmt.Errorf(fmt.Sprintf("dump of %q failed. The clone of %q cannot be created in namespace %q", cm, clusterName, toNamespace))
					}
				}
				for _, secret := range secretsToDump {
					err = dumpSecretToNamespace(clientK8s, secret, clusterToClone.GetNamespace(), toNamespace, overrideConfigMapsAndSecrets)
					if err == nil {
						reportSuccess(fmt.Sprintf("Dump secret %q to %q", secret, toNamespace))
						secretsToDelete = append(secretsToDelete, secret)
					} else {
						reportFailure(fmt.Sprintf("Dump secret %q to %q", secret, toNamespace), err)
						fmt.Printf("Deleting resources previously created.")
						// try to delete previously created cm and secrets and return an error
						deleteConfigMaps(clientK8s, configMapsToDelete, toNamespace)
						deleteSecrets(clientK8s, secretsToDelete, toNamespace)
						return fmt.Errorf(fmt.Sprintf("dump of %q failed. The clone of %q cannot be created in namespace %q", secret, clusterName, toNamespace))
					}
				}
			}
			if toNamespace != "" {
				_, err = clientCrunchy.Namespace(toNamespace).Create(context.TODO(), clone, metav1.CreateOptions{})
			} else {
				_, err = clientCrunchy.Namespace(namespace).Create(context.TODO(), clone, metav1.CreateOptions{})
			}
			if err != nil {
				reportFailure(fmt.Sprintf("creation of clone failed"), err)
				// first delete objects created previously
				if toNamespace != "" && toNamespace != clusterToClone.GetNamespace() {
					// find out if there are configmaps or secrets to be dumped
					for _, cm := range configMapsToDump {
						err = clientK8s.CoreV1().ConfigMaps(toNamespace).Delete(context.TODO(), cm, metav1.DeleteOptions{})
						switch {
						case err != nil:
							reportFailure(fmt.Sprintf("Deletion of configmap %q", cm), err)
						default:
							reportSuccess(fmt.Sprintf("Deletion of configmap %q", cm))
						}
					}
					for _, secret := range secretsToDump {
						err = clientK8s.CoreV1().Secrets(toNamespace).Delete(context.TODO(), secret, metav1.DeleteOptions{})
						switch {
						case err != nil:
							reportFailure(fmt.Sprintf("Deletion of secret %q", secret), err)
						default:
							reportSuccess(fmt.Sprintf("Deletion of secret %q", secret))
						}
					}
				}

				return errors.Wrapf(err, "failed to create clone cluster")
			}
			reportSuccess(fmt.Sprintf("Clone of cluster %q successfully created as %q", clusterName, clone.GetName()))
			fmt.Println("")

			return nil
		},
	}
	cmd.Args = cobra.ExactArgs(1)
	cmd.Flags().StringVar(&fromRepo, "from-repo", "", "repo name to clone from (repo1, repo2, etc)")
	cmd.Flags().StringVar(&toNamespace, "to-ns", "", "the target namespace where the clone will live")
	cmd.Flags().BoolVarP(&showYamlOfClone, "show-yaml", "", false, "request to show the yaml generated for the definition of the clone")
	cmd.Flags().BoolVarP(&overrideConfigMapsAndSecrets, "overrides-configs", "", false, "request to override configmaps and secrets if they already exist")

	return cmd
}

func generateCloneWithLocalStorageFrom(sourceCluster *unstructured.Unstructured, repoDataSource string, targetNamespace string) (*unstructured.Unstructured, error) {
	clone := unstructured.Unstructured{}
	clone.SetAPIVersion(sourceCluster.GetAPIVersion())
	clone.SetKind(sourceCluster.GetKind())
	clone.SetAnnotations(filterHelmManagement(sourceCluster.GetAnnotations()))
	clone.SetLabels(filterHelmManagement(sourceCluster.GetLabels()))
	clone.SetName(fmt.Sprintf("clone-%s", sourceCluster.GetName()))
	spec := make(map[string]interface{})
	switch {
	case targetNamespace == "":
		clone.SetNamespace(sourceCluster.GetNamespace())
		spec["dataSource"] = generateDataSourceSection(sourceCluster.GetName(), repoDataSource, "")
	default:
		clone.SetNamespace(targetNamespace)
		spec["dataSource"] = generateDataSourceSection(sourceCluster.GetName(), repoDataSource, sourceCluster.GetNamespace())
	}

	if repoDataSource != "" {
		if !repoIsValidForCluster(repoDataSource, sourceCluster) {
			return nil, fmt.Errorf("%q is not a valid repo for cluster %q", repoDataSource, sourceCluster)
		}
	}

	specSourceCluster := sourceCluster.Object["spec"].(map[string]interface{})
	// copy with the exact same content the following fields under spec
	for _, key := range []string{"monitoring", "openshift", "patroni", "port", "postgresVersion", "shutdown", "users"} {
		spec[key] = specSourceCluster[key]
	}
	spec["metadata"] = filterMetadata(specSourceCluster["metadata"].(map[string]interface{}))
	spec["backups"] = cloneBackupParametersButConfigureLocalStorage(specSourceCluster["backups"].(map[string]interface{}))
	spec["instances"] = cloneInstanceParametersWithoutAntiAffinity(specSourceCluster["instances"].([]interface{}))
	clone.Object["spec"] = spec
	return &clone, nil
}

func filterMetadata(metadata map[string]interface{}) interface{} {
	filtered := make(map[string]interface{})
	labels := metadata["labels"].(map[string]interface{})
	filteredLabels := make(map[string]interface{})
	for k, v := range labels {
		if k == "app.kubernetes.io/managed-by" || k == "helm.sh/chart" {
			continue
		}
		filteredLabels[k] = v
	}
	filtered["labels"] = filteredLabels

	annotations := metadata["annotations"].(map[string]interface{})
	filteredAnnotations := make(map[string]interface{})
	for k, v := range annotations {
		if k == "restarted" {
			continue
		}
		filteredAnnotations[k] = v
	}
	filtered["annotations"] = filteredLabels

	return filtered
}

func repoIsValidForCluster(repoDataSource string, sourceCluster *unstructured.Unstructured) bool {
	spec := sourceCluster.Object["spec"].(map[string]interface{})
	backups := spec["backups"].(map[string]interface{})
	pgbackrestConf := backups["pgbackrest"].(map[string]interface{})
	repos := pgbackrestConf["repos"].([]interface{})
	for _, repo := range repos {
		r := repo.(map[string]interface{})
		if r["name"] == repoDataSource {
			return true
		}
	}
	return false
}

func generateDataSourceSection(sourceClusterName, repoDataSource, sourceNamespace string) map[string]interface{} {
	dataSourceSection := make(map[string]interface{})
	postgresCluster := make(map[string]interface{})
	postgresCluster["clusterName"] = sourceClusterName
	postgresCluster["repoName"] = repoDataSource
	if sourceNamespace != "" {
		postgresCluster["clusterNamespace"] = sourceNamespace
	}
	dataSourceSection["postgresCluster"] = postgresCluster
	return dataSourceSection
}

// the main goal is to filter the helm annotations. We don't want our clone
// object to be handled by helm.
func filterHelmManagement(values map[string]string) map[string]string {
	filteredValues := make(map[string]string)
	for k, v := range values {
		if k == "app.kubernetes.io/managed-by" && v == "Helm" {
			continue
		}
		filteredValues[k] = v
	}
	return filteredValues
}

func cloneBackupParametersButConfigureLocalStorage(sourceBackupConf map[string]interface{}) map[string]interface{} {
	backupConf := make(map[string]interface{})
	pgbackrestConf := make(map[string]interface{})
	sourcePgBackrestConf := sourceBackupConf["pgbackrest"].(map[string]interface{})
	for _, key := range []string{"configuration", "global", "jobs", "manual", "metadata", "repoHost", "sidecars"} {
		pgbackrestConf[key] = sourcePgBackrestConf[key]
	}
	var repos []map[string]interface{}
	sourceRepos := sourcePgBackrestConf["repos"].([]interface{})
	schedules := make(map[string]interface{})
	// for a first version, considering that the repo1 is local and othes are
	// remote and do not require volume specification.
	for _, repo := range sourceRepos {
		m := repo.(map[string]interface{})
		// if the source is using repo1, use it as it is
		// we won't use here something different
		if m["name"] == "repo1" {
			repos = append(repos, m)
			break
		}
		// there are no repo1: keeping backup schedule of the configured repo
		schedules = m["schedules"].(map[string]interface{})
	}
	if len(repos) == 0 {
		// none was found. Adding
		repo := make(map[string]interface{})
		repo["name"] = "repo1"
		repo["schedules"] = schedules
		repos = append(repos, repo)
	}
	pgbackrestConf["repos"] = repos
	backupConf["pgbackrest"] = pgbackrestConf
	return backupConf
}

func cloneInstanceParametersWithoutAntiAffinity(sourceInstances []interface{}) []map[string]interface{} {
	var instancesList []map[string]interface{}
	for _, sourceInstance := range sourceInstances {
		si := sourceInstance.(map[string]interface{})
		instance := make(map[string]interface{})
		for _, k := range []string{"dataVolumeClaimSpec", "name", "replicas", "resources", "sidecars"} {
			instance[k] = si[k]
		}
		instancesList = append(instancesList, instance)
	}
	return instancesList
}

func requiredConfigMapsAndSecretsFor(pgCluster *unstructured.Unstructured) ([]string, []string) {
	var cmList, secretsList []string
	spec := pgCluster.Object["spec"].(map[string]interface{})
	backups := spec["backups"].(map[string]interface{})
	pgbackrest := backups["pgbackrest"].(map[string]interface{})
	configurations := pgbackrest["configuration"].([]interface{})
	for _, conf := range configurations {
		c := conf.(map[string]interface{})
		switch {
		case c["configMap"] != nil:
			cm := c["configMap"].(map[string]interface{})
			cmList = append(cmList, cm["name"].(string))
		case c["secret"] != nil:
			secret := c["secret"].(map[string]interface{})
			secretsList = append(secretsList, secret["name"].(string))
		}
	}
	return cmList, secretsList
}

func configureK8sClient(flags *genericclioptions.ConfigFlags) *kubernetes.Clientset {
	restConfig, err := flags.ToRESTConfig()
	if err != nil {
		fmt.Printf("Failed to configure access to k8s: %+v\n", err)
		os.Exit(1)
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		fmt.Printf("Failed to build a K8S client: %+v\n", err)
		os.Exit(1)
	}
	return client
}

func reportSuccess(msg string) {
	dotsCount := 80 - len(msg)
	if dotsCount < 0 {
		dotsCount = 5
	}
	fmt.Printf("%s %s [%s]\n", msg, strings.Repeat(".", dotsCount), formatterOk.Sprintf("OK"))
}

func reportFailure(msg string, err error) {
	dotsCount := 80 - len(msg)
	if dotsCount < 0 {
		dotsCount = 5
	}
	fmt.Printf("%s %s [%s]\n", msg, strings.Repeat(".", dotsCount), formatterNok.Sprintf("%s", err.Error()))
}

func dumpConfigMapToNamespace(clientK8s *kubernetes.Clientset, cmName, fromNamespace, toNamespace string, overrideIfExists bool) error {
	cm, err := clientK8s.CoreV1().ConfigMaps(fromNamespace).Get(context.TODO(), cmName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to retrieve configmap %q from namespace %q", cmName, fromNamespace)
	}
	dumpCm := corev1.ConfigMap{}
	dumpCm.SetName(cmName)
	dumpCm.SetNamespace(toNamespace)
	dumpCm.Annotations = filterHelmManagement(cm.Annotations)
	dumpCm.Labels = filterHelmManagement(cm.Labels)
	dumpCm.Data = cm.Data
	_, err = clientK8s.CoreV1().ConfigMaps(toNamespace).Create(context.TODO(), &dumpCm, metav1.CreateOptions{})
	switch {
	case err == nil:
		return nil
	case strings.Index(err.Error(), "already exists") != -1 && overrideIfExists:
		_, err = clientK8s.CoreV1().ConfigMaps(toNamespace).Update(context.TODO(), &dumpCm, metav1.UpdateOptions{})
		return err
	default:
		return errors.Wrapf(err, "failed to create dump configmap %q from ns %q to ns %q", cmName, fromNamespace, toNamespace)
	}
}

func dumpSecretToNamespace(clientK8s *kubernetes.Clientset, secretName, fromNamespace, toNamespace string, overrideIfExists bool) error {
	secret, err := clientK8s.CoreV1().Secrets(fromNamespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to retrieve secret %q from namespace %q", secretName, fromNamespace)
	}
	dump := corev1.Secret{}
	dump.SetName(secretName)
	dump.SetNamespace(toNamespace)
	dump.Annotations = filterHelmManagement(secret.Annotations)
	dump.Labels = filterHelmManagement(secret.Labels)
	dump.Data = secret.Data
	_, err = clientK8s.CoreV1().Secrets(toNamespace).Create(context.TODO(), &dump, metav1.CreateOptions{})
	switch {
	case err == nil:
		return nil
	case strings.Index(err.Error(), "already exists") != -1 && overrideIfExists:
		_, err = clientK8s.CoreV1().Secrets(toNamespace).Update(context.TODO(), &dump, metav1.UpdateOptions{})
		return err
	default:
		return errors.Wrapf(err, "failed to create dump secret %q from ns %q to ns %q", secretName, fromNamespace, toNamespace)
	}
}

// best effort to delete resources
func deleteConfigMaps(clientK8s *kubernetes.Clientset, configMapsToDelete []string, namespace string) {
	for _, cm := range configMapsToDelete {
		err := clientK8s.CoreV1().ConfigMaps(namespace).Delete(context.TODO(), cm, metav1.DeleteOptions{})
		if err != nil {
			reportFailure(fmt.Sprintf("Deletion of cm %q in ns %q", cm, namespace), err)
		} else {
			reportSuccess(fmt.Sprintf("Deletion of cm %q in ns %q", cm, namespace))
		}
	}
}

// best effort to delete resources
func deleteSecrets(clientK8s *kubernetes.Clientset, secretsToDelete []string, namespace string) {
	for _, secret := range secretsToDelete {
		err := clientK8s.CoreV1().Secrets(namespace).Delete(context.TODO(), secret, metav1.DeleteOptions{})
		if err != nil {
			reportFailure(fmt.Sprintf("Deletion of secret %q in ns %q", secret, namespace), err)
		} else {
			reportSuccess(fmt.Sprintf("Deletion of secret %q in ns %q", secret, namespace))
		}
	}
}

func createNamespaceIfNotExists(clientK8s *kubernetes.Clientset, namespace string) error {
	_, err := clientK8s.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
	if err != nil && strings.Index(err.Error(), "not found") != -1 {
		fmt.Printf("Namespace %q does not exist. Creating it.\n", namespace)
		ns := corev1.Namespace{}
		ns.Name = namespace
		_, err := clientK8s.CoreV1().Namespaces().Create(context.TODO(), &ns, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrapf(err, "failed to create namespace %q", namespace)
		}
	}
	return nil
}
