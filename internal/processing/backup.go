package processing

import (
	"context"
	"fmt"
	"github.com/crunchydata/postgres-operator-client/internal/util"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"strings"
)

func GetExistingBackups(restConfig *rest.Config, namespace, cluster, repoName, outputFormat string) (string, string, error) {
	// The only thing we need is the value after 'repo' which should be an
	// integer. If anything else is provided, we let the pgbackrest command
	// handle validation.
	repoNum := strings.TrimPrefix(repoName, "repo")

	// Get the primary instance Pod by its labels. For a Postgres cluster
	// named 'hippo', we'll use the following:
	//    postgres-operator.crunchydata.com/cluster=hippo
	//    postgres-operator.crunchydata.com/data=postgres
	//    postgres-operator.crunchydata.com/role=master

	ctx := context.Background()
	client, err := corev1.NewForConfig(restConfig)
	if err != nil {
		return "", "", err
	}

	pods, err := client.Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: util.PrimaryInstanceLabels(cluster),
	})
	if err != nil {
		return "", "", err
	}

	if len(pods.Items) != 1 {
		return "", "", fmt.Errorf("primary instance Pod not found")
	}

	PodExec, err := util.NewPodExecutor(restConfig)
	if err != nil {
		return "", "", err
	}

	// Create an executor and attempt to get the pgBackRest info output.
	exec := func(stdin io.Reader, stdout, stderr io.Writer,
		command ...string) error {
		return PodExec(pods.Items[0].GetNamespace(), pods.Items[0].GetName(),
			util.ContainerDatabase, stdin, stdout, stderr, command...)
	}

	return Executor(exec).PgBackRestInfo(outputFormat, repoNum)
}
