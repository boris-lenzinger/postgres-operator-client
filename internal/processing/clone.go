package processing

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func HasPgbackrestAdditionalConfig(clone *unstructured.Unstructured) bool {
	if clone == nil {
		return false
	}

	spec := clone.Object["spec"].(map[string]interface{})
	if spec == nil {
		return false
	}
	backups := spec["backups"].(map[string]interface{})
	if backups == nil {
		return false
	}
	pgbackrest := backups["pgbackrest"].(map[string]interface{})
	if pgbackrest == nil {
		return false
	}
	configsUntyped := pgbackrest["configuration"]
	if configsUntyped == nil {
		return false
	}
	configs := configsUntyped.([]interface{})
	for _, config := range configs {
		c := config.(map[string]interface{})
		if _, ok := c["configMap"]; ok {
			return true
		}
	}
	return false
}

func AddPgBackrestAdditionalConfigurationToClone(clone *unstructured.Unstructured, additionalConfigPgBackrest v1.ConfigMap) {
	pgbackrest := clone.Object["spec"].(map[string]interface{})["backups"].(map[string]interface{})["pgbackrest"].(map[string]interface{})
	fmt.Printf("pgbackrest : %+v\n", pgbackrest)
	if pgbackrest["configuration"] == nil {
		fmt.Println("No configuration. Creating it.")
		pgbackrest["configuration"] = []interface{}{}
	}
	configs := pgbackrest["configuration"].([]interface{})
	cmConfig := make(map[string]interface{})
	cmConfig["name"] = additionalConfigPgBackrest.Name
	m := make(map[string]interface{})
	m["configMap"] = cmConfig
	configs = append(configs, m)
	pgbackrest["configuration"] = configs
}
