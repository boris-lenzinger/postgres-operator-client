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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"regexp"
)

// holds value option repoName passed on the command line. This option determines
// from which repo we will create the clone.
var repoDataSource string

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
    postgresclusters.postgres-operator.crunchydata.com  [get create]`,
		RunE: func(cmd *cobra.Command, args []string) error {
			re := regexp.MustCompile("^repo[1-4]")
			if !re.MatchString(repoDataSource) {
				return fmt.Errorf("the repoName option must be specified and the allowed values are repo[1-4]. Other values are rejected")
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
			clone := generateCloneWithLocalStorageFrom(clusterToClone, repoDataSource)
			_, err = clientCrunchy.Namespace(namespace).Create(context.TODO(), clone, metav1.CreateOptions{})
			if err != nil {
				return errors.Wrapf(err, "failed to create clone cluster")
			}
			green := color.New(color.FgHiGreen)
			_, _ = green.Printf("Clone of cluster %q successfully created as %q", clusterName, clone.GetName())
			fmt.Println("")

			return nil
		},
	}
	cmd.Args = cobra.ExactArgs(1)
	cmd.Flags().StringVar(&repoDataSource, "repoName", "", "repoName to backup to")

	return cmd
}

func generateCloneWithLocalStorageFrom(sourceCluster *unstructured.Unstructured, repoDataSource string) *unstructured.Unstructured {
	clone := unstructured.Unstructured{}
	clone.SetAPIVersion(sourceCluster.GetAPIVersion())
	clone.SetKind(sourceCluster.GetKind())
	clone.SetAnnotations(filterHelmManagement(sourceCluster.GetAnnotations()))
	clone.SetLabels(filterHelmManagement(sourceCluster.GetLabels()))
	clone.SetName(fmt.Sprintf("clone-%s", sourceCluster.GetName()))
	clone.SetNamespace(sourceCluster.GetNamespace())
	spec := make(map[string]interface{})
	spec["dataSource"] = generateDataSourceSection(sourceCluster.GetName(), repoDataSource)
	specSourceCluster := sourceCluster.Object["spec"].(map[string]interface{})
	// copy with the exact same content the following fields under spec
	for _, key := range []string{"monitoring", "openshift", "patroni", "port", "postgresVersion", "shutdown", "users"} {
		spec[key] = specSourceCluster[key]
	}
	spec["backups"] = cloneBackupParametersButConfigureLocalStorage(specSourceCluster["backups"].(map[string]interface{}))
	spec["instances"] = cloneInstanceParametersWithoutAntiAffinity(specSourceCluster["instances"].([]interface{}))
	clone.Object["spec"] = spec
	return &clone
}

func generateDataSourceSection(sourceClusterName, repoDataSource string) map[string]interface{} {
	dataSourceSection := make(map[string]interface{})
	postgresCluster := make(map[string]interface{})
	postgresCluster["clusterName"] = sourceClusterName
	postgresCluster["repoName"] = repoDataSource
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
		if m["name"] == "repo1" {
			repos = append(repos, m)
			break
		}
		// there are no repo1: keeping backup schedule of the configured repo
		schedules = m["schedules"].(map[string]interface{})
	}
	if len(repos) == 0 {
		repo := make(map[string]interface{})
		repo["name"] = "repo1"
		repo["schedules"] = schedules
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
