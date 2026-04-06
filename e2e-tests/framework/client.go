package framework

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// ListPVCs returns PVCs from a namespace, optionally filtered by label selector.
func ListPVCs(namespace string, labelSelector string, contextName string) ([]corev1.PersistentVolumeClaim, error) {
	clientSet, err := NewClientSetForContext(contextName)
	if err != nil {
		return nil, err
	}
	pvcList, err := clientSet.CoreV1().PersistentVolumeClaims(namespace).List(
		context.Background(),
		metav1.ListOptions{
			LabelSelector: labelSelector,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("failed listing pvcs in namespace %q (selector=%q, context=%q): %w",
			namespace, labelSelector, contextName, err)
	}

	return pvcList.Items, nil

}

// NewClientSetForContext creates a Kubernetes clientset for the given kube context.
func NewClientSetForContext(contextName string) (*kubernetes.Clientset, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed building rest config for context %q: %w", contextName, err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed creating clientset for context %q: %w", contextName, err)
	}

	return clientset, nil
}

// GetClusterNodeIP returns the first schedulable node internal IP for a context.
func GetClusterNodeIP(contextName string) (string, error) {
	clientSet, err := NewClientSetForContext(contextName)
	if err != nil {
		return "", err
	}
	nodeList, err := clientSet.CoreV1().Nodes().List(
		context.Background(),
		metav1.ListOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("failed listing nodes for context %q: %w", contextName, err)
	}
	for _, node := range nodeList.Items {
		if node.Spec.Unschedulable {
			continue
		}
		for _, address := range node.Status.Addresses {
			if address.Type == corev1.NodeInternalIP {
				return address.Address, nil
			}
		}
	}
	return "", fmt.Errorf("No node IP found")
}

// ResolveUsernameForContext returns the kubeconfig auth info name (user)
// associated with the provided context. If contextName is empty, it falls back
// to current-context.
func ResolveUsernameForContext(contextName string) (string, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	rawConfig, err := loadingRules.Load()
	if err != nil {
		return "", fmt.Errorf("failed loading kubeconfig: %w", err)
	}

	ctxName := contextName
	if ctxName == "" {
		ctxName = rawConfig.CurrentContext
	}
	if ctxName == "" {
		return "", fmt.Errorf("no context name provided and current context is not set in kubeconfig")
	}

	ctx, found := rawConfig.Contexts[ctxName]
	if !found {
		return "", fmt.Errorf("context %q not found in kubeconfig", ctxName)
	}
	if ctx.AuthInfo == "" {
		return "", fmt.Errorf("no user/auth info name set for context %q", ctxName)
	}
	return ctx.AuthInfo, nil

}
