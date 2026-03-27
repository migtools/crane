package transform

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestSortResourceFiles(t *testing.T) {
	unsorted := []string{
		"resources/service.yaml",
		"resources/configmap.yaml",
		"resources/deployment.yaml",
		"resources/route.openshift.io.yaml",
	}

	sorted := SortResourceFiles(unsorted)

	expected := []string{
		"resources/configmap.yaml",
		"resources/deployment.yaml",
		"resources/route.openshift.io.yaml",
		"resources/service.yaml",
	}

	assert.Equal(t, expected, sorted)
	// Verify original is unchanged
	assert.NotEqual(t, unsorted, sorted)
}

func TestSortPatchFiles(t *testing.T) {
	unsorted := []string{
		"patches/ns2--apps-v1--Deployment--app.patch.yaml",
		"patches/ns1--v1--Service--svc.patch.yaml",
		"patches/ns1--apps-v1--Deployment--app.patch.yaml",
	}

	sorted := SortPatchFiles(unsorted)

	expected := []string{
		"patches/ns1--apps-v1--Deployment--app.patch.yaml",
		"patches/ns1--v1--Service--svc.patch.yaml",
		"patches/ns2--apps-v1--Deployment--app.patch.yaml",
	}

	assert.Equal(t, expected, sorted)
}

func TestPreserveCreationOrder(t *testing.T) {
	resources := []unstructured.Unstructured{
		{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "third"}}},
		{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "first"}}},
		{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "second"}}},
	}

	result := PreserveCreationOrder(resources)

	// Order should be preserved
	assert.Equal(t, "third", result[0].GetName())
	assert.Equal(t, "first", result[1].GetName())
	assert.Equal(t, "second", result[2].GetName())
}

func TestSortResourcesByNamespace(t *testing.T) {
	resources := []unstructured.Unstructured{
		{
			Object: map[string]interface{}{
				"kind": "Service",
				"metadata": map[string]interface{}{
					"name":      "svc-z",
					"namespace": "ns2",
				},
			},
		},
		{
			Object: map[string]interface{}{
				"kind": "Deployment",
				"metadata": map[string]interface{}{
					"name":      "deploy-a",
					"namespace": "ns1",
				},
			},
		},
		{
			Object: map[string]interface{}{
				"kind": "Service",
				"metadata": map[string]interface{}{
					"name":      "svc-a",
					"namespace": "ns1",
				},
			},
		},
		{
			Object: map[string]interface{}{
				"kind": "Deployment",
				"metadata": map[string]interface{}{
					"name":      "deploy-z",
					"namespace": "ns2",
				},
			},
		},
	}

	sorted := SortResourcesByNamespace(resources)

	// Expected order: ns1/Deployment/deploy-a, ns1/Service/svc-a, ns2/Deployment/deploy-z, ns2/Service/svc-z
	assert.Equal(t, "ns1", sorted[0].GetNamespace())
	assert.Equal(t, "Deployment", sorted[0].GetKind())
	assert.Equal(t, "deploy-a", sorted[0].GetName())

	assert.Equal(t, "ns1", sorted[1].GetNamespace())
	assert.Equal(t, "Service", sorted[1].GetKind())
	assert.Equal(t, "svc-a", sorted[1].GetName())

	assert.Equal(t, "ns2", sorted[2].GetNamespace())
	assert.Equal(t, "Deployment", sorted[2].GetKind())
	assert.Equal(t, "deploy-z", sorted[2].GetName())

	assert.Equal(t, "ns2", sorted[3].GetNamespace())
	assert.Equal(t, "Service", sorted[3].GetKind())
	assert.Equal(t, "svc-z", sorted[3].GetName())
}

func TestGetSortedKeys(t *testing.T) {
	m := map[string]interface{}{
		"zebra":     1,
		"apple":     2,
		"mango":     3,
		"banana":    4,
		"pineapple": 5,
	}

	sorted := GetSortedKeys(m)

	expected := []string{"apple", "banana", "mango", "pineapple", "zebra"}
	assert.Equal(t, expected, sorted)
}
