package processing

import (
	"context"
	"fmt"
	networkingv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	policyLabelName  = "purpose"
	policyLabelValue = "automatic-test-backup-restore"
)

func AddNetworkPoliciesIfRequired(clientK8s *kubernetes.Clientset, restConfig *rest.Config, clusterToClone *unstructured.Unstructured) error {
	err := addNativeNetworkPoliciesIfRequired(clientK8s, clusterToClone)
	// First, check if there are network policies that target the current cluster.
	if err != nil {
		return err
	}

	err = addCiliumNetworkPoliciesIfRequired(restConfig, clusterToClone)
	if err != nil {
		return err
	}

	return nil
}

func addNativeNetworkPoliciesIfRequired(clientK8s *kubernetes.Clientset, clusterToClone *unstructured.Unstructured) error {
	ns := clusterToClone.GetNamespace()

	np, err := clientK8s.NetworkingV1().NetworkPolicies(ns).List(context.TODO(), v1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list network policies %w", err)
	}
	if len(np.Items) == 0 {
		return nil
	}
	nativeNPNotPresent := true
searchForNativeNP:
	for _, policy := range np.Items {
		for k, v := range policy.Spec.PodSelector.MatchLabels {
			if k == "postgres-operator.crunchydata.com/cluster" && v == clusterToClone.GetName() {
				nativeNPNotPresent = false
				break searchForNativeNP
			}
		}
	}
	if nativeNPNotPresent {
		return nil
	}
	// Native network policy targeting the cluster found. Have to create our own
	// network policy.
	// First, create a network policy to target the cluster and accept ingress
	// rule from our clone
	allowIncomingConnexionFromClone := networkingv1.NetworkPolicy{}
	allowIncomingConnexionFromClone.ObjectMeta.Namespace = ns
	allowIncomingConnexionFromClone.ObjectMeta.Labels[policyLabelName] = policyLabelValue
	ruleName := "allow-incoming-from-clone"
	allowIncomingConnexionFromClone.ObjectMeta.Name = ruleName
	allowIncomingConnexionFromClone.Spec.PodSelector.MatchLabels = make(map[string]string)
	allowIncomingConnexionFromClone.Spec.PodSelector.MatchLabels["postgres-operator.crunchydata.com/cluster"] = clusterToClone.GetName()
	ingressRule := networkingv1.NetworkPolicyIngressRule{}
	npPeer := networkingv1.NetworkPolicyPeer{}
	npPeer.PodSelector.MatchLabels = make(map[string]string)
	npPeer.PodSelector.MatchLabels["postgres-operator.crunchydata.com/cluster"] = GenerateCloneName(clusterToClone.GetName())
	ingressRule.From = []networkingv1.NetworkPolicyPeer{npPeer}
	allowIncomingConnexionFromClone.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{ingressRule}

	_, err = clientK8s.NetworkingV1().NetworkPolicies(ns).Create(context.TODO(), &allowIncomingConnexionFromClone, v1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create rule %s due to %w", ruleName, err)
	}

	allowOutgoingFromCloneToCluster := networkingv1.NetworkPolicy{}
	allowOutgoingFromCloneToCluster.ObjectMeta.Namespace = ns
	allowOutgoingFromCloneToCluster.ObjectMeta.Labels[policyLabelName] = policyLabelValue
	ruleName = "allow-outgoing-from-clone-to-source-cluster"
	allowOutgoingFromCloneToCluster.ObjectMeta.Name = ruleName
	allowOutgoingFromCloneToCluster.Spec.PodSelector.MatchLabels = make(map[string]string)
	allowOutgoingFromCloneToCluster.Spec.PodSelector.MatchLabels["postgres-operator.crunchydata.com/cluster"] = GenerateCloneName(clusterToClone.GetName())
	egressRule := networkingv1.NetworkPolicyEgressRule{}
	npPeer = networkingv1.NetworkPolicyPeer{}
	npPeer.PodSelector.MatchLabels = make(map[string]string)
	npPeer.PodSelector.MatchLabels["postgres-operator.crunchydata.com/cluster"] = clusterToClone.GetName()
	egressRule.To = []networkingv1.NetworkPolicyPeer{npPeer}
	allowOutgoingFromCloneToCluster.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{egressRule}

	_, err = clientK8s.NetworkingV1().NetworkPolicies(ns).Create(context.TODO(), &allowOutgoingFromCloneToCluster, v1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create rule %s due to %w", ruleName, err)
	}

	return nil
}

func addCiliumNetworkPoliciesIfRequired(restConfig *rest.Config, clusterToClone *unstructured.Unstructured) error {
	// Syntax in case of ciliumnetworkpolicies
	// endpointSelector:
	//   matchLabels:
	//     postgres-operator.crunchydata.com/cluster: cedito-jahiapg
	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	cnpResource := schema.GroupVersionResource{Version: "v2", Group: "cilium.io", Resource: "CiliumNetworkPolicy"}
	ciliumNetworkPolicies, err := client.Resource(cnpResource).Namespace(clusterToClone.GetNamespace()).List(context.TODO(), v1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list cilium network policies %w", err)
	}
	// no cilium policies so we don't need to add one
	if len(ciliumNetworkPolicies.Items) == 0 {
		return nil
	}

	cnpAllowIncomingFlowFromCloneToSourceUnstructured, err := generateCiliumNetworkPolicyCloneToSourceIngress(clusterToClone)
	if err != nil {
		return err
	}

	_, err = client.Resource(cnpResource).
		Namespace(clusterToClone.GetNamespace()).
		Create(context.TODO(), cnpAllowIncomingFlowFromCloneToSourceUnstructured, v1.CreateOptions{})
	if err != nil {
		return err
	}

	cnpAllowEgressFlowFromCloneToSourceUnstructured, err := generateCiliumNetworkPolicyCloneToSourceEgress(clusterToClone)
	if err != nil {
		return err
	}

	_, err = client.Resource(cnpResource).
		Namespace(clusterToClone.GetNamespace()).
		Create(context.TODO(), cnpAllowEgressFlowFromCloneToSourceUnstructured, v1.CreateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func DeleteNetworkPoliciesIfRequired(clientK8s *kubernetes.Clientset, restConfig *rest.Config, targetNamespace string) {

	_ = clientK8s.NetworkingV1().NetworkPolicies(targetNamespace).DeleteCollection(context.TODO(),
		v1.DeleteOptions{},
		v1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s", policyLabelName, policyLabelValue)})

	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return
	}
	cnpResource := schema.GroupVersionResource{Version: "v2", Group: "cilium.io", Resource: "CiliumNetworkPolicy"}
	_ = client.Resource(cnpResource).
		Namespace(targetNamespace).
		DeleteCollection(context.TODO(),
			v1.DeleteOptions{},
			v1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s", policyLabelName, policyLabelValue)})
}

func generateCiliumNetworkPolicyCloneToSourceIngress(clusterToClone *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	var ciliumNetworkPolicy unstructured.Unstructured
	err := yaml.Unmarshal([]byte(fmt.Sprintf(`
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: free-traffic-for-source-cluster-with-clone
  labels:
    %[3]s: %[4]s
spec:
  endpointSelector:
    matchLabels:
      postgres-operator.crunchydata.com/cluster: %[1]s
  ingress:
    - fromEndpoints:
        - matchLabels:
            postgres-operator.crunchydata.com/cluster: %[2]s
  egress:
    - toEndpoints:
        - matchLabels:
            postgres-operator.crunchydata.com/cluster: %[2]s
`, clusterToClone.GetName(), GenerateCloneName(clusterToClone.GetName()), policyLabelName, policyLabelValue)), &ciliumNetworkPolicy)

	if err != nil {
		return nil, err
	}

	return &ciliumNetworkPolicy, nil
}

func generateCiliumNetworkPolicyCloneToSourceEgress(clusterToClone *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	var ciliumNetworkPolicy unstructured.Unstructured
	err := yaml.Unmarshal([]byte(fmt.Sprintf(`
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: free-traffic-for-clone-cluster-to-source
  labels:
    %[3]s: %[4]s
spec:
  endpointSelector:
    matchLabels:
      postgres-operator.crunchydata.com/cluster: %[1]s
  egress:
    - toEndpoints:
        - matchLabels:
            postgres-operator.crunchydata.com/cluster: %s
`,
		GenerateCloneName(clusterToClone.GetName()),
		clusterToClone.GetName(),
		policyLabelName,
		policyLabelValue),
	),
		&ciliumNetworkPolicy)

	if err != nil {
		return nil, err
	}

	return &ciliumNetworkPolicy, nil
}
