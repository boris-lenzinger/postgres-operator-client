package processing

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
func filterLatestConfiguration(values map[string]string) map[string]string {
	filteredValues := make(map[string]string)
	for k, v := range values {
		if k == "kubectl.kubernetes.io/last-applied-configuration" {
			continue
		}
		filteredValues[k] = v
	}
	return filteredValues
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
	filtered["annotations"] = filteredAnnotations

	return filtered
}
