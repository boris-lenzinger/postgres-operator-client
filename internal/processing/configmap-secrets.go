package processing

import (
	"context"
	"fmt"
	"github.com/crunchydata/postgres-operator-client/internal/display"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"strings"
)

func DumpConfigMapToNamespace(clientK8s *kubernetes.Clientset, cmName, fromNamespace, toNamespace string, overrideIfExists bool) error {
	cm, err := clientK8s.CoreV1().ConfigMaps(fromNamespace).Get(context.TODO(), cmName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to retrieve configmap %q from namespace %q", cmName, fromNamespace)
	}
	dumpCm := corev1.ConfigMap{}
	dumpCm.SetName(cmName)
	dumpCm.SetNamespace(toNamespace)
	dumpCm.Annotations = filterHelmManagement(cm.Annotations)
	dumpCm.Labels = filterHelmManagement(cm.Labels)
	dumpCm.Data = cm.Data
	_, err = clientK8s.CoreV1().ConfigMaps(toNamespace).Create(context.TODO(), &dumpCm, metav1.CreateOptions{})
	switch {
	case err == nil:
		return nil
	case strings.Index(err.Error(), "already exists") != -1 && overrideIfExists:
		_, err = clientK8s.CoreV1().ConfigMaps(toNamespace).Update(context.TODO(), &dumpCm, metav1.UpdateOptions{})
		return err
	default:
		return errors.Wrapf(err, "failed to create dump configmap %q from ns %q to ns %q", cmName, fromNamespace, toNamespace)
	}
}

func DumpSecretToNamespace(clientK8s *kubernetes.Clientset, secretName, fromNamespace, toNamespace string, overrideIfExists bool) error {
	secret, err := clientK8s.CoreV1().Secrets(fromNamespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to retrieve secret %q from namespace %q", secretName, fromNamespace)
	}
	dump := corev1.Secret{}
	dump.SetName(secretName)
	dump.SetNamespace(toNamespace)
	dump.Annotations = filterHelmManagement(secret.Annotations)
	dump.Labels = filterHelmManagement(secret.Labels)
	dump.Data = secret.Data
	_, err = clientK8s.CoreV1().Secrets(toNamespace).Create(context.TODO(), &dump, metav1.CreateOptions{})
	switch {
	case err == nil:
		return nil
	case strings.Index(err.Error(), "already exists") != -1 && overrideIfExists:
		_, err = clientK8s.CoreV1().Secrets(toNamespace).Update(context.TODO(), &dump, metav1.UpdateOptions{})
		return err
	default:
		return errors.Wrapf(err, "failed to create dump secret %q from ns %q to ns %q", secretName, fromNamespace, toNamespace)
	}
}

// DeleteConfigMaps makes the best effort to delete resources and does not when
// an error occur. The reason is that this is mainly used in the process of
// deleting resources after a clone failure. So we need to delete as many as
// possible of the resources we have created previously
func DeleteConfigMaps(clientK8s *kubernetes.Clientset, configMapsToDelete []string, namespace string) {
	for _, cm := range configMapsToDelete {
		err := clientK8s.CoreV1().ConfigMaps(namespace).Delete(context.TODO(), cm, metav1.DeleteOptions{})
		if err != nil {
			display.ReportFailure(fmt.Sprintf("Deletion of configmap %q in ns %q", cm, namespace), err)
		} else {
			display.ReportSuccess(fmt.Sprintf("Deletion of configmap %q in ns %q", cm, namespace))
		}
	}
}

// DeleteSecrets makes the best effort to delete resources and does not when
// an error occur. The reason is that this is mainly used in the process of
// deleting resources after a clone failure. So we need to delete as many as
// possible of the resources we have created previously
func DeleteSecrets(clientK8s *kubernetes.Clientset, secretsToDelete []string, namespace string) {
	for _, secret := range secretsToDelete {
		err := clientK8s.CoreV1().Secrets(namespace).Delete(context.TODO(), secret, metav1.DeleteOptions{})
		if err != nil {
			display.ReportFailure(fmt.Sprintf("Deletion of secret %q in ns %q", secret, namespace), err)
		} else {
			display.ReportSuccess(fmt.Sprintf("Deletion of secret %q in ns %q", secret, namespace))
		}
	}
}

func DumpConfigMapsAndSecretsIfNeeded(clientK8s *kubernetes.Clientset, clusterToClone *unstructured.Unstructured, targetNamespace string, overrideConfigMapsAndSecrets bool) ([]string, []string, error) {
	// find out if there are configmaps or secrets to be dumped
	var configMapsToDelete, secretsToDelete []string
	var err error
	configMapsToDump, secretsToDump := computeRequiredConfigMapsAndSecretsFor(clusterToClone)
	for _, cm := range configMapsToDump {
		err = DumpConfigMapToNamespace(clientK8s, cm, clusterToClone.GetNamespace(), targetNamespace, overrideConfigMapsAndSecrets)
		if err == nil {
			display.ReportSuccess(fmt.Sprintf("Dump configmap %q to %q", cm, targetNamespace))
			configMapsToDelete = append(configMapsToDelete, cm)
		} else {
			display.ReportFailure(fmt.Sprintf("Dump configmap %q to %q", cm, targetNamespace), err)
			fmt.Printf("Deleting resources previously created.")
			// try to delete them and return an error
			DeleteConfigMaps(clientK8s, configMapsToDelete, targetNamespace)
			return nil, nil, fmt.Errorf(fmt.Sprintf("dump of %q failed. The clone of %q cannot be created in namespace %q", cm, clusterToClone.GetName(), targetNamespace))
		}
	}
	for _, secret := range secretsToDump {
		err = DumpSecretToNamespace(clientK8s, secret, clusterToClone.GetNamespace(), targetNamespace, overrideConfigMapsAndSecrets)
		if err == nil {
			display.ReportSuccess(fmt.Sprintf("Dump secret %q to %q", secret, targetNamespace))
			secretsToDelete = append(secretsToDelete, secret)
		} else {
			display.ReportFailure(fmt.Sprintf("Dump secret %q to %q", secret, targetNamespace), err)
			fmt.Printf("Deleting resources previously created.")
			// try to delete previously created cm and secrets and return an error
			DeleteConfigMaps(clientK8s, configMapsToDelete, targetNamespace)
			DeleteSecrets(clientK8s, secretsToDelete, targetNamespace)
			return nil, nil, fmt.Errorf(fmt.Sprintf("dump of %q failed. The clone of %q cannot be created in namespace %q", secret, clusterToClone.GetName(), targetNamespace))
		}
	}
	return configMapsToDelete, secretsToDelete, nil
}
