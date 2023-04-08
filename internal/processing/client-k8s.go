package processing

import (
	"github.com/pkg/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func ConfigureK8sClient(flags *genericclioptions.ConfigFlags) (*kubernetes.Clientset, *rest.Config, error) {
	restConfig, err := flags.ToRESTConfig()
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get rest config")
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to build a K8S client")
	}
	return client, restConfig, nil
}
