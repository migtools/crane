package file

import (
	"sort"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestGetResourceOrder(t *testing.T) {
	tests := []struct {
		kind          string
		expectedOrder int
	}{
		{"Namespace", 10},
		{"CustomResourceDefinition", 20},
		{"Secret", 230},
		{"ConfigMap", 240},
		{"Role", 300},
		{"RoleBinding", 310},
		{"Deployment", 340},
		{"Service", 400},
		{"UnknownKind", 999}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			order := GetResourceOrder(tt.kind)
			if order != tt.expectedOrder {
				t.Errorf("GetResourceOrder(%s) = %d, want %d", tt.kind, order, tt.expectedOrder)
			}
		})
	}
}

func TestGetResourceOrder_RoleBeforeRoleBinding(t *testing.T) {
	// This is the key test for issue #266
	roleOrder := GetResourceOrder("Role")
	roleBindingOrder := GetResourceOrder("RoleBinding")

	if roleOrder >= roleBindingOrder {
		t.Errorf("Role order (%d) should be less than RoleBinding order (%d)", roleOrder, roleBindingOrder)
	}
}

func TestGetResourceOrder_ConfigMapBeforeDeployment(t *testing.T) {
	configMapOrder := GetResourceOrder("ConfigMap")
	deploymentOrder := GetResourceOrder("Deployment")

	if configMapOrder >= deploymentOrder {
		t.Errorf("ConfigMap order (%d) should be less than Deployment order (%d)", configMapOrder, deploymentOrder)
	}
}

func TestGetResourceOrder_WebhooksAfterWorkloads(t *testing.T) {
	// Webhooks must come after workloads to avoid bootstrap deadlock
	deploymentOrder := GetResourceOrder("Deployment")
	serviceOrder := GetResourceOrder("Service")
	validatingWebhookOrder := GetResourceOrder("ValidatingWebhookConfiguration")
	mutatingWebhookOrder := GetResourceOrder("MutatingWebhookConfiguration")

	if validatingWebhookOrder <= deploymentOrder {
		t.Errorf("ValidatingWebhookConfiguration order (%d) should be greater than Deployment order (%d) to avoid bootstrap deadlock",
			validatingWebhookOrder, deploymentOrder)
	}

	if validatingWebhookOrder <= serviceOrder {
		t.Errorf("ValidatingWebhookConfiguration order (%d) should be greater than Service order (%d) to avoid bootstrap deadlock",
			validatingWebhookOrder, serviceOrder)
	}

	if mutatingWebhookOrder <= deploymentOrder {
		t.Errorf("MutatingWebhookConfiguration order (%d) should be greater than Deployment order (%d) to avoid bootstrap deadlock",
			mutatingWebhookOrder, deploymentOrder)
	}

	if mutatingWebhookOrder <= serviceOrder {
		t.Errorf("MutatingWebhookConfiguration order (%d) should be greater than Service order (%d) to avoid bootstrap deadlock",
			mutatingWebhookOrder, serviceOrder)
	}
}

func TestGetOrderedResourceFilename(t *testing.T) {
	tests := []struct {
		name     string
		obj      unstructured.Unstructured
		expected string
	}{
		{
			name: "Role resource",
			obj: unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "rbac.authorization.k8s.io/v1",
					"kind":       "Role",
					"metadata": map[string]interface{}{
						"name":      "pod-reader",
						"namespace": "default",
					},
				},
			},
			expected: "300_Role_rbac.authorization.k8s.io_v1_default_pod-reader.yaml",
		},
		{
			name: "RoleBinding resource",
			obj: unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "rbac.authorization.k8s.io/v1",
					"kind":       "RoleBinding",
					"metadata": map[string]interface{}{
						"name":      "pod-reader-binding",
						"namespace": "default",
					},
				},
			},
			expected: "310_RoleBinding_rbac.authorization.k8s.io_v1_default_pod-reader-binding.yaml",
		},
		{
			name: "ConfigMap resource",
			obj: unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "my-config",
						"namespace": "default",
					},
				},
			},
			expected: "240_ConfigMap__v1_default_my-config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename := GetOrderedResourceFilename(tt.obj)
			if filename != tt.expected {
				t.Errorf("GetOrderedResourceFilename() = %s, want %s", filename, tt.expected)
			}
		})
	}
}

func TestOrderValues_MaxThreeDigits(t *testing.T) {
	// Ensure all order values are ≤ 999 to maintain 3-digit padding
	// This prevents the sorting bug where 4-digit values (e.g., 1000) sort
	// lexicographically before 3-digit values (e.g., 240)
	for kind, order := range ResourceOrder {
		if order > 999 {
			t.Errorf("Order value for %s is %d, but must be ≤ 999 to maintain 3-digit padding. "+
				"Either reduce the order value or switch to 4-digit padding (%%04d)", kind, order)
		}
	}
}

func TestOrderedFilenames_LexicographicSorting(t *testing.T) {
	// Verify that ordered filenames sort lexicographically in the same order as numeric ordering
	// This is critical for tools that apply resources in sorted filename order
	testResources := []struct {
		kind     string
		name     string
		order    int
	}{
		{"Namespace", "ns1", 10},
		{"ConfigMap", "cm1", 240},
		{"Role", "role1", 300},
		{"Deployment", "deploy1", 340},
		{"Service", "svc1", 400},
		{"MutatingWebhookConfiguration", "mwc1", 810},
		{"UnknownKind", "unknown1", 999},
	}

	var filenames []string
	for _, r := range testResources {
		obj := unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       r.kind,
				"metadata": map[string]interface{}{
					"name":      r.name,
					"namespace": "default",
				},
			},
		}
		filename := GetOrderedResourceFilename(obj)
		filenames = append(filenames, filename)
	}

	// Create a sorted copy
	sorted := make([]string, len(filenames))
	copy(sorted, filenames)
	sort.Strings(sorted)

	// Verify lexicographic sorting matches numeric ordering
	for i := range filenames {
		if filenames[i] != sorted[i] {
			t.Errorf("Filename ordering mismatch at index %d:\n  expected: %s\n  got:      %s\n  Lexicographic sort does not match numeric ordering",
				i, filenames[i], sorted[i])
		}
	}

	// Specifically verify UnknownKind (999) comes last
	lastFile := filenames[len(filenames)-1]
	if !strings.HasPrefix(lastFile, "999_") {
		t.Errorf("UnknownKind should be last (999_...), but got: %s", lastFile)
	}
}

func TestAlphabeticalSorting_RoleVsRoleBinding(t *testing.T) {
	// This test demonstrates the issue #266 problem
	role := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "Role",
			"metadata": map[string]interface{}{
				"name":      "pod-reader",
				"namespace": "default",
			},
		},
	}

	roleBinding := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "RoleBinding",
			"metadata": map[string]interface{}{
				"name":      "pod-reader-binding",
				"namespace": "default",
			},
		},
	}

	roleFilename := GetOrderedResourceFilename(role)
	roleBindingFilename := GetOrderedResourceFilename(roleBinding)

	// With ordered filenames, Role should come before RoleBinding alphabetically
	if roleFilename >= roleBindingFilename {
		t.Errorf("Role filename (%s) should sort before RoleBinding filename (%s)",
			roleFilename, roleBindingFilename)
	}

	// Verify the prefix numbers are correct
	if roleFilename[:3] != "300" {
		t.Errorf("Role filename should start with '300', got: %s", roleFilename[:3])
	}
	if roleBindingFilename[:3] != "310" {
		t.Errorf("RoleBinding filename should start with '310', got: %s", roleBindingFilename[:3])
	}
}
