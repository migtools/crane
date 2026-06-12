package file

import (
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
		{"UnknownKind", 1000}, // Default
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
