package processing

import (
	"fmt"
	yamlv2 "gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"testing"
)

func buildPostgresClusterWithPgBackrestAdditionalConfig() *unstructured.Unstructured {
	clusterDef := `apiVersion: postgres-operator.crunchydata.com/v1beta1
kind: PostgresCluster
metadata:
  name: ref-cluster
spec:
  postgresVersion: 14
  instances:
    - name: instance1
      dataVolumeClaimSpec:
        accessModes: [ReadWriteOnce]
        resources: { requests: { storage: 1Gi } }
  backups:
    pgbackrest:
      configuration:
      - configMap:
          name: cm-pgbackrest-additionalconfig
      repos:
      - name: repo1
        volume:
          volumeClaimSpec:
            accessModes: [ReadWriteOnce]
            resources: { requests: { storage: 1Gi } }
`

	cluster := unstructured.Unstructured{Object: make(map[string]interface{})}
	err := yaml.Unmarshal([]byte(clusterDef), &cluster)
	if err != nil {
		panic(fmt.Sprintf("failed to use YAML as input due to %+v", err.Error()))
	}
	return &cluster
}

func buildPostgresClusterWithPgBackrestAdditionalConfigAsSecret() *unstructured.Unstructured {
	clusterDef := `apiVersion: postgres-operator.crunchydata.com/v1beta1
kind: PostgresCluster
metadata:
  name: ref-cluster
spec:
  postgresVersion: 14
  instances:
    - name: instance1
      dataVolumeClaimSpec:
        accessModes: [ReadWriteOnce]
        resources: { requests: { storage: 1Gi } }
  backups:
    pgbackrest:
      configuration:
      - secret:
          name: secret-pgbackrest-additionalconfig
      repos:
      - name: repo1
        volume:
          volumeClaimSpec:
            accessModes: [ReadWriteOnce]
            resources: { requests: { storage: 1Gi } }
`
	cluster := unstructured.Unstructured{Object: make(map[string]interface{})}
	err := yaml.Unmarshal([]byte(clusterDef), &cluster)
	if err != nil {
		panic(fmt.Sprintf("failed to use YAML as input due to %+v", err.Error()))
	}
	return &cluster
}

func buildPostgresClusterWithPgBackrestAdditionalConfigAsSecretAndS3() *unstructured.Unstructured {
	clusterDef := `apiVersion: postgres-operator.crunchydata.com/v1beta1
kind: PostgresCluster
metadata:
  name: ref-cluster
spec:
  postgresVersion: 14
  instances:
    - name: instance1
      dataVolumeClaimSpec:
        accessModes: [ReadWriteOnce]
        resources: { requests: { storage: 1Gi } }
  backups:
    pgbackrest:
      configuration:
      - secret:
          name: secret-pgbackrest-additionalconfig
      global:
        repo2-path: /
        repo2-retention-diff: "3"
        repo2-retention-full: "3"
        repo2-s3-uri-style: path
        repo2-storage-ca-file: /etc/pgbackrest/conf.d/ca-bundle.crt
        repo2-storage-verify-tls: "y"
      manual:
        options:
        - --type=full
        repoName: repo2
      repos:
      - name: repo1
        volume:
          volumeClaimSpec:
            accessModes: [ReadWriteOnce]
            resources: { requests: { storage: 1Gi } }
      - name: repo2
        s3:
          bucket: bucket-name
          endpoint: s3-endpoint
          region: aws-region
        schedules:
          differential: 16 19 * * 3
          full: 41 0 * * 0
`
	cluster := unstructured.Unstructured{Object: make(map[string]interface{})}
	err := yaml.Unmarshal([]byte(clusterDef), &cluster)
	if err != nil {
		panic(fmt.Sprintf("failed to use YAML as input due to %+v", err.Error()))
	}
	return &cluster
}

func buildPostgresClusterWithoutPgBackrestAdditionalConfig() *unstructured.Unstructured {
	clusterDef := `apiVersion: postgres-operator.crunchydata.com/v1beta1
kind: PostgresCluster
metadata:
  name: ref-cluster
spec:
  postgresVersion: 14
  instances:
    - name: instance1
      dataVolumeClaimSpec:
        accessModes: [ReadWriteOnce]
        resources: { requests: { storage: 1Gi } }
  backups:
    pgbackrest:
      repos:
      - name: repo1
        volume:
          volumeClaimSpec:
            accessModes: [ReadWriteOnce]
            resources: { requests: { storage: 1Gi } }`
	var cluster unstructured.Unstructured
	_ = yaml.Unmarshal([]byte(clusterDef), &cluster)
	return &cluster
}

func TestHasPgbackrestAdditionalConfig(t *testing.T) {
	type args struct {
		clone *unstructured.Unstructured
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Detection that PG cluster has an additional config",
			args: args{
				clone: buildPostgresClusterWithPgBackrestAdditionalConfig(),
			},
			want: true,
		},
		{
			name: "Detection that PG Cluster has no additional config",
			args: args{
				clone: buildPostgresClusterWithoutPgBackrestAdditionalConfig(),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasPgbackrestAdditionalConfig(tt.args.clone); got != tt.want {
				t.Errorf("HasPgbackrestAdditionalConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddPgBackrestAdditionalConfigurationToClone(t *testing.T) {
	cloneToAugment := buildPostgresClusterWithoutPgBackrestAdditionalConfig()
	additionalConfig := v1.ConfigMap{}
	additionalConfig.SetName("config-pgbackrest")
	fmt.Printf("Clone to augment before : %+v\n", cloneToAugment)
	AddPgBackrestAdditionalConfigurationToClone(cloneToAugment, additionalConfig)
	fmt.Println("============")
	content, err := yamlv2.Marshal(cloneToAugment)
	if err != nil {
		t.Errorf("failed to marshal object to YAML due to %+v", err)
	}
	fmt.Printf("Clone augmented after : %s\n", string(content))
	fmt.Println("============")
	type args struct {
		clone                      *unstructured.Unstructured
		additionalConfigPgBackrest v1.ConfigMap
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Check cluster with no pgbackrest config is augmented",
			args: args{
				clone:                      cloneToAugment,
				additionalConfigPgBackrest: additionalConfig,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasPgbackrestAdditionalConfig(tt.args.clone); got != tt.want {
				t.Errorf("HasPgbackrestAdditionalConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateCloneDefinitionFromClusterWithS3(t *testing.T) {
	sourceCluster := buildPostgresClusterWithPgBackrestAdditionalConfigAsSecretAndS3()
	clone, err := GenerateCloneDefinitionWithLocalStorageFrom(sourceCluster, "repo2", "targetns", "2023-01-01", true)
	if err != nil {
		t.Error(fmt.Sprintf("failed to clone source cluster due to %s", err.Error()))
	}
	content, err := yamlv2.Marshal(clone)
	if err != nil {
		t.Error(fmt.Sprintf("failed to marshal clone as yaml : %s", err.Error()))
	}
	fmt.Printf("YAML :\n%s\n", string(content))
}
