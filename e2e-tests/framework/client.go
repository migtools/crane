package framework

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// ListPVCs returns PersistentVolumeClaims from a namespace, optionally filtered
// by label selector, using the provided kubeconfig context.
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

// VerifyPVCsExistByName checks that all source PVCs exist by name in the target PVC list.
// Returns an error listing all missing PVCs if any are not found in the target list.
func VerifyPVCsExistByName(sourcePVCs, targetPVCs []corev1.PersistentVolumeClaim) error {
	// Build a set of target PVC names for O(1) lookup
	targetNames := make(map[string]bool, len(targetPVCs))
	for _, tgtPVC := range targetPVCs {
		targetNames[tgtPVC.Name] = true
	}

	// Collect all missing PVC names
	var missing []string
	for _, srcPVC := range sourcePVCs {
		if !targetNames[srcPVC.Name] {
			missing = append(missing, srcPVC.Name)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("source PVCs not found in target: %v", missing)
	}
	return nil
}

// NewClientSetForContext builds a client-go clientset scoped to the provided
// kubeconfig context name.
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

// GetClusterNodeIP returns the first schedulable node internal IP visible from
// the provided kubeconfig context.
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
	return "", fmt.Errorf("no schedulable node with InternalIP found for context %q", contextName)
}

// ResolveUsernameForContext resolves the Kubernetes username represented by a
// kubeconfig context for use in RBAC subjects.
//
// Resolution order:
//  1. Client certificate CN — used by minikube cert-based contexts; this is the
//     identity Kubernetes RBAC evaluates directly.
//  2. SelfSubjectReview API — used by OCP token-based contexts (HTPasswd, OIDC,
//     etc.); asks the API server "who am I?" which is the authoritative answer.
//  3. AuthInfo key name up to the first "/" — last-resort fallback that handles
//     kubeconfigs where the auth-info key is set to the bare username (e.g. "dev").
//
// If contextName is empty, it falls back to current-context.
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

	authInfo, found := rawConfig.AuthInfos[ctx.AuthInfo]
	if !found {
		return "", fmt.Errorf("auth info %q referenced by context %q not found in kubeconfig", ctx.AuthInfo, ctxName)
	}

	// 1. Prefer certificate CN — this is the user identity evaluated by RBAC on
	// minikube and any cluster using client-cert authentication.
	var certBytes []byte
	if len(authInfo.ClientCertificateData) > 0 {
		certBytes = authInfo.ClientCertificateData
	} else if authInfo.ClientCertificate != "" {
		certBytes, err = os.ReadFile(authInfo.ClientCertificate)
		if err != nil {
			return "", fmt.Errorf(
				"failed reading client certificate file %q for context %q (auth info %q): %w",
				authInfo.ClientCertificate, ctxName, ctx.AuthInfo, err,
			)
		}
	}

	if len(certBytes) > 0 {
		cert, err := parseClientCertificate(certBytes)
		if err != nil {
			return "", fmt.Errorf(
				"failed parsing client certificate for context %q (auth info %q): %w",
				ctxName, ctx.AuthInfo, err,
			)
		}
		if cert.Subject.CommonName == "" {
			return "", fmt.Errorf(
				"client certificate for context %q (auth info %q) has empty subject common name",
				ctxName, ctx.AuthInfo,
			)
		}
		return cert.Subject.CommonName, nil
	}

	// 2. Token-based context (OCP HTPasswd / OIDC / service-account token).
	// Ask the API server who the token belongs to via SelfSubjectReview.
	// This is available on Kubernetes ≥ 1.28 and all OCP 4.x versions.
	if authInfo.Token != "" || authInfo.TokenFile != "" {
		username, err := resolveUsernameViaSelfSubjectReview(ctxName)
		if err == nil {
			return username, nil
		}
		// Non-fatal: fall through to the key-name heuristic so that
		// environments without the API (e.g. older clusters) still work.
	}

	// 3. Last resort: use the auth-info key name, trimming everything from the
	// first "/" onwards. OCP kubeconfigs typically produce keys like
	// "dev/api-cluster-...:6443"; the part before "/" is the bare username.
	keyName := ctx.AuthInfo
	if idx := strings.Index(keyName, "/"); idx != -1 {
		keyName = keyName[:idx]
	}
	return keyName, nil
}

// resolveUsernameViaSelfSubjectReview calls the SelfSubjectReview API using the
// credentials of the given context and returns the username the API server
// resolved. Available on Kubernetes ≥ 1.28 and all OCP 4.x.
func resolveUsernameViaSelfSubjectReview(contextName string) (string, error) {
	clientSet, err := NewClientSetForContext(contextName)
	if err != nil {
		return "", fmt.Errorf("failed building clientset for SelfSubjectReview: %w", err)
	}

	review, err := clientSet.AuthenticationV1().SelfSubjectReviews().Create(
		context.Background(),
		&authenticationv1.SelfSubjectReview{},
		metav1.CreateOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("SelfSubjectReview failed for context %q: %w", contextName, err)
	}

	username := review.Status.UserInfo.Username
	if username == "" {
		return "", fmt.Errorf("SelfSubjectReview returned empty username for context %q", contextName)
	}
	return username, nil
}

// parseClientCertificate parses a single X.509 client certificate from kubeconfig
// certificate bytes. It accepts PEM bundles and falls back to DER parsing.
func parseClientCertificate(certBytes []byte) (*x509.Certificate, error) {
	rest := certBytes
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = remaining
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse PEM certificate block: %w", err)
		}
		return cert, nil
	}

	// Some kubeconfigs may store DER bytes directly.
	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate bytes as PEM or DER: %w", err)
	}
	return cert, nil
}