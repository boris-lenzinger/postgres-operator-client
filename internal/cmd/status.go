// Copyright 2021 - 2023 Crunchy Data Solutions, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"context"
	"fmt"
	"github.com/crunchydata/postgres-operator-client/internal"
	"github.com/crunchydata/postgres-operator-client/internal/apis/postgres-operator.crunchydata.com/v1beta1"
	"github.com/crunchydata/postgres-operator-client/internal/processing"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"io"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"regexp"
	"strconv"
	"strings"
)

var regexRestoreFile = regexp.MustCompile(`^.* restore file /[^ ]* \([^ ]+ (?P<percentage>[0-9]{1,3}\.[0-9]{1,2})%\)( checksum [a-zA-Z0-9]{40,})?`)

var logsDecorator = color.New(color.FgHiYellow)
var percentDecorator = color.New(color.FgHiMagenta)
var okDecorator = color.New(color.FgHiGreen)
var titleDecorator = color.New(color.FgHiMagenta)

// newStatusCommand returns the status subcommand of the PGO plugin.
// Subcommands of status command will be used to check if clusters are running
// fine and, in case of synchronization of a pod, report the percentage of
// progression.
func newStatusCommand(config *internal.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Reports status of a cluster (or all clusters).",
		Long: `Reports status of a cluster (or all clusters).

#### RBAC Requirements
    Resources                                           Verbs
    ---------                                           -----
    postgresclusters.postgres-operator.crunchydata.com  [get,list]
    pods/log                                            [get,list,create]
`,
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && !allClusters {
			return fmt.Errorf("you did not supply a cluster name and you did not use option --all. Cannot guess which cluster should be checked.")
		}

		ctx := context.Background()

		var clustersList []unstructured.Unstructured

		_, clientCrunchy, err := v1beta1.NewPostgresClusterClient(config)
		if err != nil {
			return err
		}

		namespace := *config.ConfigFlags.Namespace

		if allClusters {
			l, err := clientCrunchy.Namespace("").List(ctx, metav1.ListOptions{})
			if err != nil {
				return fmt.Errorf("failed to list all crunchy clusters due to %s", err.Error())
			}
			clustersList = l.Items
		} else {
			clusterName := args[0]
			clusterToUpdate, err := clientCrunchy.Namespace(namespace).Get(context.TODO(), clusterName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get cluster %q in namespace %q due to %s", clusterName, namespace, err.Error())
			}
			clustersList = []unstructured.Unstructured{*clusterToUpdate}
		}

		clientK8s, _, err := processing.ConfigureK8sClient(config.ConfigFlags)
		if err != nil {
			return err
		}

		// check status of clusters
		fmt.Println(titleDecorator.Sprintf("Status of clusters :"))
		count := len(clustersList)
		padding := len(strconv.Itoa(count))
		for i, c := range clustersList {
			fmt.Printf("(%0*d/%0*d) %s / %s : %s\n", padding, i+1, padding, count, logsDecorator.Sprintf(c.GetNamespace()), logsDecorator.Sprintf(c.GetName()), getStatus(*clientK8s, c))
		}

		return nil
	}

	// kubectl-pgo replica-count clustername --value 3
	// kubectl-pgo replica-count --all
	cmd.Flags().IntVar(&replicaCountValue, "value", 2, "Set the number of nodes in a postgres cluster. 0 and below are rejected. Above 4 is rejected too.")
	cmd.Flags().BoolVar(&allClusters, "all", false, "Is used to require to modify all clusters.")

	return cmd
}

func getStatus(clientK8s kubernetes.Clientset, c unstructured.Unstructured) string {
	status := c.Object["status"].(map[string]interface{})
	instances := status["instances"].([]interface{})
	instance0 := instances[0].(map[string]interface{})
	readyReplicas := instance0["readyReplicas"].(int64)
	replicas := instance0["replicas"].(int64)
	switch {
	case readyReplicas != replicas:
		// retrieve logs and analyze logs
		notReadyPod, err := identifyPodNotReady(clientK8s, &c)
		if err != nil {
			return fmt.Sprintf("failed to identify pod not ready due to %s", err.Error())
		}
		logs, err := getLogsOfPod(clientK8s, notReadyPod, c.GetNamespace(), "database")
		if err != nil {
			return fmt.Sprintf("failed to get logs of container database in pod %s due to %s", notReadyPod, err.Error())
		}
		statusInLogs := analyzeLogs(logs)
		return fmt.Sprintf("In progress. Replicas : %d, Ready Replicas : %d (%s)", replicas, readyReplicas, statusInLogs)
	case readyReplicas == replicas:
		return okDecorator.Sprintf("All replicas are ready !")
	}
	return ""
}

func identifyPodNotReady(clientK8s kubernetes.Clientset, c *unstructured.Unstructured) (string, error) {
	// get pods with label postgres-operator.crunchydata.com/cluster: clustername
	// in the namespace. Then check if the status.containerStatuses[*].name == database a son champ ready à true.
	pods, err := clientK8s.CoreV1().Pods(c.GetNamespace()).List(context.TODO(), metav1.ListOptions{LabelSelector: fmt.Sprintf("postgres-operator.crunchydata.com/cluster=%s", c.GetName())})
	if err != nil {
		return "", err
	}
	for _, p := range pods.Items {
		containerStatuses := p.Status.ContainerStatuses
		for _, st := range containerStatuses {
			if st.Name != "database" {
				continue
			}
			if st.Ready {
				continue
			}
			return p.Name, nil
		}
	}
	return "", fmt.Errorf("pod with database not ready not found")
}

func getLogsOfPod(clientK8s kubernetes.Clientset, podName, namespace, containerName string) (string, error) {
	lines := int64(1000)
	req := clientK8s.CoreV1().Pods(namespace).GetLogs(podName, &v1.PodLogOptions{Container: "database", TailLines: &lines})
	rc, err := req.Stream(context.TODO())
	if err != nil {
		return "", err
	}
	content, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func analyzeLogs(logContent string) string {
	lines := strings.Split(logContent, "\n")

	if len(lines) == 1 {
		return logsDecorator.Sprintf(lines[0])
	}
	var percent string
	for _, l := range lines {
		match := regexRestoreFile.FindStringSubmatch(l)
		if len(match) > 1 {
			percent = match[1]
			continue
		}
	}
	if percent == "" {
		return logsDecorator.Sprintf(lines[len(lines)-1])
	}
	return fmt.Sprintf("%s %s%%", logsDecorator.Sprintf("Rebuild of database is done at"), percentDecorator.Sprintf("%s", percent))
}
