package framework

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"slices"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
)

// mtaIDPattern matches MTA ticket references like [MTA-801] in spec descriptions.
var mtaIDPattern = regexp.MustCompile(`\[MTA-\d+\]`)

// ExtractMTAID extracts the MTA-XXX identifier (without brackets) from a spec's full text.
// Returns an empty string if no MTA ID is found.
func ExtractMTAID(fullText string) string {
	match := mtaIDPattern.FindString(fullText)
	return strings.Trim(match, "[]")
}

// RegisterMTAResultReporter registers a ReportAfterEach hook that prints a
// [CRANE-RESULT] line for every spec whose description contains an MTA ID,
// so CI can parse and correlate results back to Polarion/MTA tickets.
func RegisterMTAResultReporter() {
	ginkgo.ReportAfterEach(func(report types.SpecReport) {
		id := ExtractMTAID(report.FullText())
		if id == "" {
			return
		}
		switch report.State {
		case types.SpecStatePassed:
			fmt.Printf("\n[CRANE-RESULT] PASSED %s\n", id)
		case types.SpecStateFailed, types.SpecStateTimedout, types.SpecStatePanicked:
			fmt.Printf("\n[CRANE-RESULT] FAILED %s\n", id)
		case types.SpecStateSkipped:
			fmt.Printf("\n[CRANE-RESULT] SKIPPED %s\n", id)
		}
	})
}

type ExpectedClusterRoleBinding struct {
	ClusterRoleBindingName string
	ClusterRoleName        string
	SubjectName            string
}

func ValidateClusterRBAC(kubectl KubectlRunner, bindings []ExpectedClusterRoleBinding) error {
	clusterRoles := map[string]bool{}
	for _, b := range bindings {
		clusterRoles[b.ClusterRoleName] = true
	}
	for cr := range clusterRoles {
		if _, err := kubectl.Run("get", "clusterrole", cr); err != nil {
			return fmt.Errorf("ClusterRole %s not found: %w", cr, err)
		}
		log.Printf("ClusterRole %s exists", cr)
	}

	for _, b := range bindings {
		if _, err := kubectl.Run("get", "clusterrolebinding", b.ClusterRoleBindingName); err != nil {
			return fmt.Errorf("ClusterRoleBinding %s not found: %w", b.ClusterRoleBindingName, err)
		}

		roleRef, err := kubectl.Run("get", "clusterrolebinding", b.ClusterRoleBindingName, "-o", "jsonpath={.roleRef.name}")
		if err != nil {
			return fmt.Errorf("failed to get roleRef for CRB %s: %w", b.ClusterRoleBindingName, err)
		}
		if roleRef != b.ClusterRoleName {
			return fmt.Errorf("CRB %s references %s, expected %s", b.ClusterRoleBindingName, roleRef, b.ClusterRoleName)
		}

		subjectOutput, err := kubectl.Run("get", "clusterrolebinding", b.ClusterRoleBindingName, "-o", "jsonpath={.subjects[*].name}")
		if err != nil {
			return fmt.Errorf("failed to get subject for CRB %s: %w", b.ClusterRoleBindingName, err)
		}
		subjects := strings.Fields(subjectOutput)

		if !slices.Contains(subjects, b.SubjectName) {
			return fmt.Errorf("CRB %s subject is %s, expected %s", b.ClusterRoleBindingName, subjectOutput, b.SubjectName)
		}
		log.Printf("CRB %s -> CR %s (subject: %s) verified", b.ClusterRoleBindingName, b.ClusterRoleName, b.SubjectName)
	}
	return nil
}

// VerifySecret fetches a secret by name from the given namespace and cluster, parses the JSON response,
// and verifies the type field matches the expected value and, when expectedData is non-nil, that every
// key's base64-encoded value matches exactly. Returns a descriptive error if the secret cannot be fetched,
// the JSON cannot be parsed, the type does not match, or any data key is missing/mismatched.
func VerifySecret(kubectl KubectlRunner, namespace, secretName, expectedType string, expectedData map[string]string) error {
	secretJson, err := kubectl.Run("get", "secret", secretName, "-n", namespace, "-o", "json")
	if err != nil {
		return fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}
	var secretObj map[string]any
	if err := json.Unmarshal([]byte(secretJson), &secretObj); err != nil {
		return fmt.Errorf("failed to parse secret %s JSON: %w", secretName, err)
	}
	actualType, ok := secretObj["type"].(string)
	if !ok {
		return fmt.Errorf("secret %s/%s: type field is not a string, got %T", namespace, secretName, secretObj["type"])
	}
	if actualType != expectedType {
		return fmt.Errorf("secret %s/%s: expected type %q but got %q", namespace, secretName, expectedType, actualType)
	}
	data, ok := secretObj["data"].(map[string]any)
	if !ok || len(data) == 0 {
		return fmt.Errorf("secret %s/%s: data field is missing or empty, got %T", namespace, secretName, secretObj["data"])
	}
	for key, expectedValue := range expectedData {
		actualValue, ok := data[key].(string)
		if !ok {
			return fmt.Errorf("secret %s/%s: expected data key %q not found or not a string", namespace, secretName, key)
		}
		if actualValue != expectedValue {
			return fmt.Errorf("secret %s/%s: data key %q does not match source value", namespace, secretName, key)
		}
	}
	log.Printf("Secret verified: name=%s type=%s\n", secretName, actualType)
	return nil
}

// GetSecretData fetches a secret by name and returns its data map with values left base64-encoded,
// exactly as returned by the Kubernetes API, suitable for direct comparison via VerifySecret's
// expectedData parameter without needing to decode/re-encode.
func GetSecretData(kubectl KubectlRunner, namespace, secretName string) (map[string]string, error) {
	secretJson, err := kubectl.Run("get", "secret", secretName, "-n", namespace, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}
	var secretObj struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(secretJson), &secretObj); err != nil {
		return nil, fmt.Errorf("failed to parse secret %s JSON: %w", secretName, err)
	}
	return secretObj.Data, nil
}

// PodVolumeMount maps a PVC to a mount path inside a VerifierPodOptions pod.
type PodVolumeMount struct {
	PVCName   string
	MountPath string
}

// VerifierPodOptions configures a disposable pod created by DeployVerifierPod.
type VerifierPodOptions struct {
	Name      string
	Namespace string
	Image     string
	Command   []string
	Volumes   []PodVolumeMount
	// Labels are applied to the pod's metadata. Useful when the pod needs to
	// match a Deployment's selector (e.g. simulating the real app briefly)
	// rather than just being a throwaway inspector.
	Labels map[string]string
}

// DeployVerifierPod creates a disposable pod that mounts one or more existing
// PVCs, so their data can be inspected directly (e.g. via kubectl exec)
// independent of whatever application normally owns them. This is commonly
// needed mid-migration: a PVC has been transferred to the target cluster, but
// the owning application hasn't been deployed there yet, so there's nothing
// else to mount it and let you look inside.
//
// PVCs are typically ReadWriteOnce, so callers must call DeleteVerifierPod
// before mounting the same PVC anywhere else (a subsequent transfer-pvc run,
// or the real app).
func DeployVerifierPod(k KubectlRunner, opts VerifierPodOptions) error {
	var labelLines strings.Builder
	for key, val := range opts.Labels {
		fmt.Fprintf(&labelLines, "    %s: %q\n", key, val)
	}
	labelsBlock := ""
	if labelLines.Len() > 0 {
		labelsBlock = "  labels:\n" + labelLines.String()
	}

	var mounts, volumes strings.Builder
	for i, v := range opts.Volumes {
		name := fmt.Sprintf("vol%d", i)
		fmt.Fprintf(&mounts, "    - name: %s\n      mountPath: %s\n", name, v.MountPath)
		fmt.Fprintf(&volumes, "  - name: %s\n    persistentVolumeClaim:\n      claimName: %s\n", name, v.PVCName)
	}
	mountsBlock, volumesBlock := "", ""
	if len(opts.Volumes) > 0 {
		mountsBlock = "    volumeMounts:\n" + mounts.String()
		volumesBlock = "  volumes:\n" + volumes.String()
	}

	commandJSON, err := json.Marshal(opts.Command)
	if err != nil {
		return fmt.Errorf("marshal verifier pod command: %w", err)
	}

	manifest := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
%sspec:
  restartPolicy: Never
  containers:
  - name: verifier
    image: %s
    command: %s
%s%s`, opts.Name, opts.Namespace, labelsBlock, opts.Image, string(commandJSON), mountsBlock, volumesBlock)

	if err := k.ApplyYAMLSpec(manifest, opts.Namespace); err != nil {
		return fmt.Errorf("apply verifier pod %s/%s: %w", opts.Namespace, opts.Name, err)
	}
	if _, err := k.Run("wait", "--for=condition=Ready", "pod/"+opts.Name, "-n", opts.Namespace, "--timeout=120s"); err != nil {
		return fmt.Errorf("wait for verifier pod %s/%s to become ready: %w", opts.Namespace, opts.Name, err)
	}
	return nil
}

// DeleteVerifierPod deletes a pod created by DeployVerifierPod. Safe to call
// even if the pod was already removed (e.g. by an earlier DeferCleanup).
func DeleteVerifierPod(k KubectlRunner, namespace, name string) error {
	if _, err := k.Run("delete", "pod", name, "-n", namespace, "--ignore-not-found", "--wait=true", "--timeout=120s"); err != nil {
		return fmt.Errorf("delete verifier pod %s/%s: %w", namespace, name, err)
	}
	return nil
}

// ReadFileFromPod returns trimmed file contents from a pod via kubectl exec.
func ReadFileFromPod(k KubectlRunner, namespace, podName, filePath string) (string, error) {
	out, err := k.Run("exec", "-n", namespace, podName, "--", "cat", filePath)
	if err != nil {
		return "", fmt.Errorf("read file %q from pod %s/%s: %w", filePath, namespace, podName, err)
	}
	return strings.TrimSpace(StripKubectlWarnings(out)), nil
}

// SeedPodManifest creates a temporary pod that seeds known test data into a PVC.
func SeedPodManifest(namespace, podName, pvcName string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  restartPolicy: Never
  containers:
    - name: seed
      image: busybox:1.36
      command:
        - sh
        - -c
        - |
          set -eux
          mkdir -p /data/testdir
          echo 'hello-from-source' > /data/hello.txt
          echo 'unattached-pvc-check' > /data/testdir/nested.txt
          date -u +"%%Y-%%m-%%dT%%H:%%M:%%SZ" > /data/timestamp.txt
          sleep 3600
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: %s
`, podName, namespace, pvcName)
}

// VerifyPodManifest creates a temporary pod that mounts a PVC for verification.
func VerifyPodManifest(namespace, podName, pvcName string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  restartPolicy: Never
  containers:
    - name: verify
      image: busybox:1.36
      command:
        - sh
        - -c
        - |
          set -eux
          ls -R /data
          sleep 3600
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: %s
`, podName, namespace, pvcName)
}
