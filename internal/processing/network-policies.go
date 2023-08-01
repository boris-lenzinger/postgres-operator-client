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

func AddNetworkPoliciesIfRequired(clientK8s *kubernetes.Clientset, restConfig *rest.Config, clusterToClone *unstructured.Unstructured, cloneName string) error {
	err := addNativeNetworkPoliciesIfRequired(clientK8s, clusterToClone, cloneName)
	// First, check if there are network policies that target the current cluster.
	if err != nil {
		return err
	}

	err = addCiliumNetworkPoliciesIfRequired(restConfig, clusterToClone, cloneName)
	if err != nil {
		return err
	}

	return nil
}

func addNativeNetworkPoliciesIfRequired(clientK8s *kubernetes.Clientset, clusterToClone *unstructured.Unstructured, cloneName string) error {
	ns := clusterToClone.GetNamespace()

	np, err := clientK8s.NetworkingV1().NetworkPolicies(ns).List(context.TODO(), v1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list network policies %w", err)
	}
	if len(np.Items) == 0 {
		return nil
	}
	fmt.Println("Found network policy in namespace. Creating network policy to give access to clone pods to source cluster.")
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
	ruleName := fmt.Sprintf("%s-allow-incoming-from-%s", clusterToClone.GetName(), cloneName)
	allowIncomingConnexionFromClone.ObjectMeta.Name = ruleName
	allowIncomingConnexionFromClone.Spec.PodSelector.MatchLabels = make(map[string]string)
	allowIncomingConnexionFromClone.Spec.PodSelector.MatchLabels["postgres-operator.crunchydata.com/cluster"] = clusterToClone.GetName()
	ingressRule := networkingv1.NetworkPolicyIngressRule{}
	npPeer := networkingv1.NetworkPolicyPeer{}
	npPeer.PodSelector.MatchLabels = make(map[string]string)
	npPeer.PodSelector.MatchLabels["postgres-operator.crunchydata.com/cluster"] = cloneName
	ingressRule.From = []networkingv1.NetworkPolicyPeer{npPeer}
	allowIncomingConnexionFromClone.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{ingressRule}

	_, err = clientK8s.NetworkingV1().NetworkPolicies(ns).Create(context.TODO(), &allowIncomingConnexionFromClone, v1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create rule %s due to %w", ruleName, err)
	}

	allowOutgoingFromCloneToCluster := networkingv1.NetworkPolicy{}
	allowOutgoingFromCloneToCluster.ObjectMeta.Namespace = ns
	allowOutgoingFromCloneToCluster.ObjectMeta.Labels[policyLabelName] = policyLabelValue
	ruleName = fmt.Sprintf("allow-outgoing-from-%s-to-%s", cloneName, clusterToClone.GetName())
	allowOutgoingFromCloneToCluster.ObjectMeta.Name = ruleName
	allowOutgoingFromCloneToCluster.Spec.PodSelector.MatchLabels = make(map[string]string)
	allowOutgoingFromCloneToCluster.Spec.PodSelector.MatchLabels["postgres-operator.crunchydata.com/cluster"] = cloneName
	egressRule := networkingv1.NetworkPolicyEgressRule{}
	npPeer = networkingv1.NetworkPolicyPeer{}
	npPeer.PodSelector.MatchLabels = make(map[string]string)
	npPeer.PodSelector.MatchLabels["postgres-operator.crunchydata.com/cluster"] = clusterToClone.GetName()
	toCoreDNS := networkingv1.NetworkPolicyPeer{}
	toCoreDNS.PodSelector.MatchLabels = make(map[string]string)
	toCoreDNS.PodSelector.MatchLabels["k8s-app"] = "kube-dns"
	egressRule.To = []networkingv1.NetworkPolicyPeer{npPeer, toCoreDNS}
	allowOutgoingFromCloneToCluster.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{egressRule}

	_, err = clientK8s.NetworkingV1().NetworkPolicies(ns).Create(context.TODO(), &allowOutgoingFromCloneToCluster, v1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create rule %s due to %w", ruleName, err)
	}

	return nil
}

func addCiliumNetworkPoliciesIfRequired(restConfig *rest.Config, clusterToClone *unstructured.Unstructured, cloneName string) error {
	// Syntax in case of ciliumnetworkpolicies
	// endpointSelector:
	//   matchLabels:
	//     postgres-operator.crunchydata.com/cluster: cedito-jahiapg
	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	// warning : keep the resource name in lowercase else the kubernetes API
	// will report a 404 not found
	cnpResource := schema.GroupVersionResource{Version: "v2", Group: "cilium.io", Resource: "ciliumnetworkpolicies"}
	ciliumNetworkPolicies, err := client.Resource(cnpResource).Namespace(clusterToClone.GetNamespace()).List(context.TODO(), v1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list cilium network policies %w", err)
	}
	// no cilium policies so we don't need to add one
	if len(ciliumNetworkPolicies.Items) == 0 {
		return nil
	}

	fmt.Printf("%d Cilium network policies detected. Adding our own to get access to the cluster\n", len(ciliumNetworkPolicies.Items))
	fmt.Printf("Those policies are labeled with %s=%s so you can delete them easily.\n", policyLabelName, policyLabelValue)

	cnpAllowIncomingFlowFromCloneToSourceUnstructured, err := generateCiliumNetworkPolicyCloneToSourceIngress(clusterToClone, cloneName)
	if err != nil {
		return err
	}

	_, err = client.Resource(cnpResource).
		Namespace(clusterToClone.GetNamespace()).
		Create(context.TODO(), cnpAllowIncomingFlowFromCloneToSourceUnstructured, v1.CreateOptions{})
	if err != nil {
		return err
	}

	cnpAllowEgressFlowFromCloneToSourceUnstructured, err := generateCiliumNetworkPolicyCloneToSourceEgress(clusterToClone, cloneName)
	if err != nil {
		return err
	}

	_, err = client.Resource(cnpResource).
		Namespace(clusterToClone.GetNamespace()).
		Create(context.TODO(), cnpAllowEgressFlowFromCloneToSourceUnstructured, v1.CreateOptions{})
	if err != nil {
		return err
	}

	allowClusterIntraPodCommunication := generateCiliumNetworkPolicyCloneIntra(GenerateCloneName(clusterToClone.GetName()))

	_, err = client.Resource(cnpResource).
		Namespace(clusterToClone.GetNamespace()).
		Create(context.TODO(), allowClusterIntraPodCommunication, v1.CreateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func generateCiliumNetworkPolicyCloneIntra(cloneName string) *unstructured.Unstructured {
	var ciliumNetworkPolicy unstructured.Unstructured
	_ = yaml.Unmarshal([]byte(fmt.Sprintf(`
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: %[1]s-free-intra-cluster                                           
spec:
  egress:
  - toEndpoints:
    - matchLabels:
        postgres-operator.crunchydata.com/cluster: %[1]s
  - toEndpoints:
    - matchLabels:
        io.kubernetes.pod.namespace: kube-system
        k8s-app: kube-dns
    toPorts:
    - ports:
      - port: "53"
        protocol: UDP
  - toEntities:
    - cluster
    toPorts:
    - ports:
      - port: "6443"
  - toEntities:
    - world
    toPorts:
    - ports:
      - port: "443"
  endpointSelector:
    matchLabels:
      postgres-operator.crunchydata.com/cluster: %[1]s           
  ingress:
  - fromEndpoints:
    - matchLabels:
        postgres-operator.crunchydata.com/cluster: %[1]s`, cloneName)), &ciliumNetworkPolicy)

	return &ciliumNetworkPolicy
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

func generateCiliumNetworkPolicyCloneToSourceIngress(clusterToClone *unstructured.Unstructured, cloneName string) (*unstructured.Unstructured, error) {
	var ciliumNetworkPolicy unstructured.Unstructured
	err := yaml.Unmarshal([]byte(fmt.Sprintf(`
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: free-traffic-from%[1]s-with-%[2]s
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
    - toEndpoints:
        - matchLabels:
            io.kubernetes.pod.namespace: kube-system
            k8s-app: kube-dns
      toPorts:
        - ports:
            - port: "53"
              protocol: UDP
    - toEntities:
        - cluster
      toPorts:
        - ports:
            - port: "6443"
    - toEntities:
        - world
      toPorts:
        - ports:
            - port: "443"
`, clusterToClone.GetName(), cloneName, policyLabelName, policyLabelValue)), &ciliumNetworkPolicy)

	if err != nil {
		return nil, err
	}

	return &ciliumNetworkPolicy, nil
}

func generateCiliumNetworkPolicyCloneToSourceEgress(clusterToClone *unstructured.Unstructured, cloneName string) (*unstructured.Unstructured, error) {
	var ciliumNetworkPolicy unstructured.Unstructured
	err := yaml.Unmarshal([]byte(fmt.Sprintf(`
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: free-traffic-for-%[1]s-to-%[2]s
  labels:
    %[3]s: %[4]s
spec:
  endpointSelector:
    matchLabels:
      postgres-operator.crunchydata.com/cluster: %[1]s
  egress:
    - toEndpoints:
        - matchLabels:
            postgres-operator.crunchydata.com/cluster: %[2]s
    - toEndpoints:
        - matchLabels:
            io.kubernetes.pod.namespace: kube-system
            k8s-app: kube-dns
      toPorts:
        - ports:
            - port: "53"
              protocol: UDP
          rules:
            dns:
              - matchPattern: "*"
`,
		cloneName,
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
