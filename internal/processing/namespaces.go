package processing

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"strings"
)

func CreateNamespaceIfNotExists(clientK8s *kubernetes.Clientset, namespace string) error {
	_, err := clientK8s.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
	if err != nil && strings.Index(err.Error(), "not found") != -1 {
		fmt.Printf("Namespace %q does not exist. Creating it.\n", namespace)
		ns := corev1.Namespace{}
		ns.Name = namespace
		_, err := clientK8s.CoreV1().Namespaces().Create(context.TODO(), &ns, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrapf(err, "failed to create namespace %q", namespace)
		}
	}
	return nil
}
