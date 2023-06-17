package processing

import (
	"fmt"
	yamlv2 "gopkg.in/yaml.v2"
	"testing"
)

func TestGenerateVerboseConfigForPgBackrest(t *testing.T) {
	c := GenerateVerboseConfigForPgBackrest()
	content, err := yamlv2.Marshal(c)
	if err != nil {
		t.Errorf("failed to transform configmap to YAML due to %s", err.Error())
	}
	fmt.Printf("Content of configmap as YAML :\n%s\n", string(content))
}
