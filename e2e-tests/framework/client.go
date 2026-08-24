package framework

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	nodeaffinity "k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
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

// GetPVC returns a PersistentVolumeClaim by name.
func GetPVC(contextName, namespace, name string) (*corev1.PersistentVolumeClaim, error) {
	clientSet, err := NewClientSetForContext(contextName)
	if err != nil {
		return nil, err
	}
	pvc, err := clientSet.CoreV1().PersistentVolumeClaims(namespace).Get(
		context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed getting PVC %s/%s (context=%q): %w", namespace, name, contextName, err)
	}
	return pvc, nil
}

// PVCStorageClassName returns the raw storageClassName from a PVC spec, or
// empty when the field is unset or explicitly set to "".
func PVCStorageClassName(pvc corev1.PersistentVolumeClaim) string {
	if pvc.Spec.StorageClassName != nil {
		return *pvc.Spec.StorageClassName
	}
	return ""
}

// DefaultStorageClassName returns the name of the cluster default StorageClass.
func DefaultStorageClassName(contextName string) (string, error) {
	clientSet, err := NewClientSetForContext(contextName)
	if err != nil {
		return "", err
	}
	list, err := clientSet.StorageV1().StorageClasses().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed listing StorageClasses (context=%q): %w", contextName, err)
	}
	for i := range list.Items {
		if list.Items[i].Annotations["storageclass.kubernetes.io/is-default-class"] == "true" {
			return list.Items[i].Name, nil
		}
	}
	return "", fmt.Errorf("no default StorageClass found (context=%q)", contextName)
}

// ResolvePVCStorageClass returns the PVC's StorageClass, falling back to the
// cluster default when spec.storageClassName is unset.
func ResolvePVCStorageClass(contextName string, pvc corev1.PersistentVolumeClaim) (string, error) {
	if pvc.Spec.StorageClassName != nil {
		return *pvc.Spec.StorageClassName, nil
	}
	return DefaultStorageClassName(contextName)
}

// PrepareDestinationStorageClass returns a destination StorageClass name for
// conversion tests. It prefers the first existing class whose name differs from
// sourceName. If no alternative class exists, it falls back to cloning
// sourceName into fallbackCloneName and returns a cleanup callback for the
// temporary clone.
func PrepareDestinationStorageClass(contextName, sourceName, fallbackCloneName string) (string, func() error, error) {
	if sourceName == "" {
		return "", nil, fmt.Errorf("source StorageClass name is empty")
	}
	if fallbackCloneName == "" {
		return "", nil, fmt.Errorf("fallback clone StorageClass name is empty")
	}
	if fallbackCloneName == sourceName {
		return "", nil, fmt.Errorf("fallback clone StorageClass %q must differ from source %q", fallbackCloneName, sourceName)
	}

	clientSet, err := NewClientSetForContext(contextName)
	if err != nil {
		return "", nil, err
	}

	list, err := clientSet.StorageV1().StorageClasses().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return "", nil, fmt.Errorf("failed listing StorageClasses (context=%q): %w", contextName, err)
	}
	for i := range list.Items {
		if list.Items[i].Name != sourceName {
			return list.Items[i].Name, func() error { return nil }, nil
		}
	}

	created, err := CloneStorageClass(contextName, sourceName, fallbackCloneName)
	if err != nil {
		return "", nil, err
	}
	if !created {
		return fallbackCloneName, func() error { return nil }, nil
	}
	cleanup := func() error {
		return DeleteStorageClass(contextName, fallbackCloneName)
	}
	return fallbackCloneName, cleanup, nil
}

// CloneStorageClass creates destName as a copy of sourceName's provisioner and
// volume settings, without copying default-class annotations. It returns true
// only when it created destName in this call. Source and dest names must differ.
func CloneStorageClass(contextName, sourceName, destName string) (bool, error) {
	if sourceName == "" {
		return false, fmt.Errorf("source StorageClass name is empty")
	}
	if destName == "" {
		return false, fmt.Errorf("destination StorageClass name is empty")
	}
	if sourceName == destName {
		return false, fmt.Errorf("destination StorageClass %q must differ from source %q", destName, sourceName)
	}

	clientSet, err := NewClientSetForContext(contextName)
	if err != nil {
		return false, err
	}
	ctx := context.Background()
	src, err := clientSet.StorageV1().StorageClasses().Get(ctx, sourceName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get source StorageClass %q: %w", sourceName, err)
	}

	_, err = clientSet.StorageV1().StorageClasses().Get(ctx, destName, metav1.GetOptions{})
	if err == nil {
		return false, nil
	}
	if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("get destination StorageClass %q: %w", destName, err)
	}

	clone := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: destName,
			Labels: map[string]string{
				"app.konveyor.io/e2e": "true",
			},
		},
		Provisioner:          src.Provisioner,
		Parameters:           src.Parameters,
		ReclaimPolicy:        src.ReclaimPolicy,
		MountOptions:         src.MountOptions,
		AllowVolumeExpansion: src.AllowVolumeExpansion,
		VolumeBindingMode:    src.VolumeBindingMode,
		AllowedTopologies:    src.AllowedTopologies,
	}
	_, err = clientSet.StorageV1().StorageClasses().Create(ctx, clone, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return false, nil
		}
		return false, fmt.Errorf("create StorageClass %q cloned from %q: %w", destName, sourceName, err)
	}
	return true, nil
}

// DeleteStorageClass deletes a StorageClass, treating NotFound as success.
func DeleteStorageClass(contextName, name string) error {
	if name == "" {
		return nil
	}
	clientSet, err := NewClientSetForContext(contextName)
	if err != nil {
		return err
	}
	err = clientSet.StorageV1().StorageClasses().Delete(context.Background(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete StorageClass %q: %w", name, err)
	}
	return nil
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

// VerifyPVCHasData mounts a PVC in a temporary pod and checks that the mount
// path is non-empty. Returns an error if empty, which typically indicates a
// failed rsync transfer.
func VerifyPVCHasData(kubectl KubectlRunner, namespace, pvcName, mountPath string) error {
	podName := fmt.Sprintf("pvc-check-%s", pvcName)
	podSpec := map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":      podName,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"restartPolicy": "Never",
			"containers": []map[string]any{{
				"name":    "check",
				"image":   "busybox",
				"command": []string{"sleep", "600"},
				"volumeMounts": []map[string]any{{
					"name":      "data",
					"mountPath": mountPath,
				}},
			}},
			"volumes": []map[string]any{{
				"name": "data",
				"persistentVolumeClaim": map[string]any{
					"claimName": pvcName,
				},
			}},
		},
	}

	specJSON, err := json.Marshal(podSpec)
	if err != nil {
		return fmt.Errorf("marshal pod spec: %w", err)
	}

	_, err = kubectl.RunWithStdin(string(specJSON), "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("create inspector pod for PVC %s: %w", pvcName, err)
	}
	defer func() {
		if _, delErr := kubectl.Run("delete", "pod", podName, "-n", namespace, "--ignore-not-found=true"); delErr != nil {
			log.Printf("cleanup: failed to delete inspector pod %s: %v", podName, delErr)
		}
	}()

	var lastPhase string
	var lastErr error
	ready := false
	for i := 0; i < 30; i++ {
		out, err := kubectl.Run("get", "pod", podName, "-n", namespace,
			"-o", "jsonpath={.status.phase}")
		lastPhase = out
		lastErr = err
		if err == nil && out == "Running" {
			ready = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !ready {
		if lastErr != nil {
			return fmt.Errorf("inspector pod %s/%s for PVC %s not ready: %w", namespace, podName, pvcName, lastErr)
		}
		return fmt.Errorf("inspector pod %s/%s for PVC %s not ready: phase=%s", namespace, podName, pvcName, lastPhase)
	}

	out, err := kubectl.Run("exec", podName, "-n", namespace, "--",
		"find", mountPath, "-mindepth", "1", "-maxdepth", "1",
		"!", "-name", "lost+found", "-print")
	if err != nil {
		return fmt.Errorf("exec into inspector pod for PVC %s: %w", pvcName, err)
	}
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("PVC %s/%s is empty at %s after transfer — rsync may have failed silently", namespace, pvcName, mountPath)
	}
	log.Printf("PVC %s/%s has data at %s: %d entries", namespace, pvcName, mountPath, len(strings.Split(strings.TrimSpace(out), "\n")))
	return nil
}

// VerifyPVCSchedulable checks that the PV bound to a PVC has node affinity
// that at least one cluster node satisfies. Returns an error immediately if no
// node matches, instead of waiting for a pod scheduling timeout downstream.
func VerifyPVCSchedulable(contextName, namespace, pvcName string) error {
	clientSet, err := NewClientSetForContext(contextName)
	if err != nil {
		return err
	}

	pvc, err := clientSet.CoreV1().PersistentVolumeClaims(namespace).Get(
		context.Background(), pvcName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get PVC %s/%s: %w", namespace, pvcName, err)
	}
	pvName := pvc.Spec.VolumeName
	if pvName == "" {
		return fmt.Errorf("PVC %s/%s is not bound to a PV", namespace, pvcName)
	}

	pv, err := clientSet.CoreV1().PersistentVolumes().Get(
		context.Background(), pvName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get PV %s: %w", pvName, err)
	}

	if pv.Spec.NodeAffinity == nil || pv.Spec.NodeAffinity.Required == nil {
		return nil
	}

	selector, err := nodeaffinity.NewNodeSelector(pv.Spec.NodeAffinity.Required)
	if err != nil {
		return fmt.Errorf("parse node affinity for PV %s: %w", pvName, err)
	}

	nodeList, err := clientSet.CoreV1().Nodes().List(
		context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list nodes: %w", err)
	}

	for i := range nodeList.Items {
		if nodeList.Items[i].Spec.Unschedulable {
			continue
		}
		if selector.Match(&nodeList.Items[i]) {
			return nil
		}
	}

	return fmt.Errorf(
		"PV %s bound to PVC %s/%s has node affinity that no cluster node satisfies — "+
			"check node labels and topology on target cluster (context %s)",
		pvName, namespace, pvcName, contextName)
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
