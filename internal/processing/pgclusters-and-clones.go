package processing

import (
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GenerateCloneDefinitionWithLocalStorageFrom creates an in-memory object containing
// the values to then generate the clone in the kubernetes cluster.
//   - sourceCluster is the original cluster from which the code builds a clone
//   - repoDataSource is the repo used to generate the clone
//   - targetNamespace is where the clone must be built. If empty, indicates the same
//     namespace as the original cluster
func GenerateCloneDefinitionWithLocalStorageFrom(sourceCluster *unstructured.Unstructured, repoDataSource string, targetNamespace, pitr string) (*unstructured.Unstructured, error) {
	clone := unstructured.Unstructured{}
	clone.SetAPIVersion(sourceCluster.GetAPIVersion())
	clone.SetKind(sourceCluster.GetKind())
	clone.SetAnnotations(filterHelmManagement(sourceCluster.GetAnnotations()))
	clone.SetLabels(filterHelmManagement(sourceCluster.GetLabels()))
	clone.SetName(fmt.Sprintf("clone-%s", sourceCluster.GetName()))
	spec := make(map[string]interface{})
	if targetNamespace == "" {
		targetNamespace = sourceCluster.GetNamespace()
	}
	clone.SetNamespace(targetNamespace)
	spec["dataSource"] = generateDataSourceSection(sourceCluster.GetName(), repoDataSource, sourceCluster.GetNamespace(), pitr)

	if repoDataSource != "" {
		if !repoExistsForCluster(repoDataSource, sourceCluster) {
			return nil, fmt.Errorf("%q is not a valid repo for cluster %q", repoDataSource, sourceCluster)
		}
	}

	specSourceCluster := sourceCluster.Object["spec"].(map[string]interface{})
	// copy with the exact same content the following fields under spec
	for _, key := range []string{"monitoring", "openshift", "patroni", "port", "postgresVersion", "shutdown", "users"} {
		spec[key] = specSourceCluster[key]
	}
	if specSourceCluster["metadata"] != nil {
		spec["metadata"] = filterMetadata(specSourceCluster["metadata"].(map[string]interface{}))
	}
	spec["backups"] = cloneBackupParametersButConfigureLocalStorage(specSourceCluster["backups"].(map[string]interface{}))
	spec["instances"] = cloneInstanceParametersWithoutAntiAffinity(specSourceCluster["instances"].([]interface{}))
	clone.Object["spec"] = spec
	return &clone, nil
}

func generateDataSourceSection(sourceClusterName, repoDataSource, sourceNamespace, pitr string) map[string]interface{} {
	dataSourceSection := make(map[string]interface{})
	postgresCluster := make(map[string]interface{})
	postgresCluster["clusterName"] = sourceClusterName
	postgresCluster["repoName"] = repoDataSource
	if sourceNamespace != "" {
		postgresCluster["clusterNamespace"] = sourceNamespace
	}
	if pitr != "" {
		//options:
		//	- --type=time
		//	- --target="2021-06-09 14:15:11-04"
		options := make([]string, 2)
		options[0] = "--type=time"
		options[1] = fmt.Sprintf("--target=\"%s\"", pitr)
		postgresCluster["options"] = options
	}
	dataSourceSection["postgresCluster"] = postgresCluster
	return dataSourceSection
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
	// considering that a clone has only local storage for
	// backups. If the original cluster has no local storage,
	// the code keeps the current backup schedules and generates
	// a new local repository. Size for local repo is 3x the size
	// of an instance.
	for _, repo := range sourceRepos {
		m := repo.(map[string]interface{})
		// if the source is using repo1, use it as it is
		// we won't use here something different
		if m["name"] == "repo1" {
			repos = append(repos, m)
			break
		}
		// there are no repo1: keeping backup schedule of the configured repo
		if m["schedules"] != nil {
			schedules = m["schedules"].(map[string]interface{})
		}
	}
	if len(repos) == 0 {
		// none was found. Adding
		repo := make(map[string]interface{})
		repo["name"] = "repo1"
		repo["schedules"] = schedules
		// TODO : set the size !!
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

func computeRequiredConfigMapsAndSecretsFor(pgCluster *unstructured.Unstructured) ([]string, []string) {
	var cmList, secretsList []string
	spec := pgCluster.Object["spec"].(map[string]interface{})
	backups := spec["backups"].(map[string]interface{})
	var pgbackrest map[string]interface{}
	if backups["pgbackrest"] != nil {
		pgbackrest = backups["pgbackrest"].(map[string]interface{})
	}
	var configurations []interface{}
	if pgbackrest["configuration"] != nil {
		configurations = pgbackrest["configuration"].([]interface{})
	}
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

func repoExistsForCluster(repoDataSource string, sourceCluster *unstructured.Unstructured) bool {
	spec := sourceCluster.Object["spec"].(map[string]interface{})
	backups := spec["backups"].(map[string]interface{})
	var pgbackrestConf map[string]interface{}
	if backups["pgbackrest"] != nil {
		pgbackrestConf = backups["pgbackrest"].(map[string]interface{})
	}
	repos := pgbackrestConf["repos"].([]interface{})
	for _, repo := range repos {
		r := repo.(map[string]interface{})
		if r["name"] == repoDataSource {
			return true
		}
	}
	return false
}
