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
