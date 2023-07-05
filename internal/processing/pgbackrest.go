package processing

import (
	v1 "k8s.io/api/core/v1"
)

func GenerateVerboseConfigForPgBackrest() v1.ConfigMap {
	cm := v1.ConfigMap{}
	data := make(map[string]string, 1)
	data["additionalConfig.conf"] = ` |
            [global]
            io-timeout=1800
            log-level-console=detail
`
	cm.Data = data
	cm.Name = "pgbackrest-additional-config"
	return cm
}
