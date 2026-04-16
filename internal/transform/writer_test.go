package transform

import (
	"testing"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestFilterValidRemoveOps(t *testing.T) {
	tests := []struct {
		name           string
		resource       map[string]interface{}
		patches        string
		expectedOps    int
		expectFiltered bool
	}{
		{
			name: "remove existing field",
			resource: map[string]interface{}{
				"metadata": map[string]interface{}{
					"uid":             "abc-123",
					"resourceVersion": "12345",
				},
			},
			patches: `[
				{"op": "remove", "path": "/metadata/uid"},
				{"op": "remove", "path": "/metadata/resourceVersion"}
			]`,
			expectedOps:    2,
			expectFiltered: false,
		},
		{
			name: "remove non-existent field",
			resource: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "test",
				},
			},
			patches: `[
				{"op": "remove", "path": "/metadata/uid"}
			]`,
			expectedOps:    0,
			expectFiltered: true,
		},
		{
			name: "remove non-existent spec field",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"type": "ClusterIP",
				},
			},
			patches: `[
				{"op": "remove", "path": "/spec/externalIPs"}
			]`,
			expectedOps:    0,
			expectFiltered: true,
		},
		{
			name: "mixed existing and non-existent",
			resource: map[string]interface{}{
				"metadata": map[string]interface{}{
					"uid":  "abc-123",
					"name": "test",
				},
				"status": map[string]interface{}{
					"phase": "Running",
				},
			},
			patches: `[
				{"op": "remove", "path": "/metadata/uid"},
				{"op": "remove", "path": "/metadata/resourceVersion"},
				{"op": "remove", "path": "/status"}
			]`,
			expectedOps:    2,
			expectFiltered: true,
		},
		{
			name: "add operation not filtered",
			resource: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "test",
				},
			},
			patches: `[
				{"op": "add", "path": "/metadata/labels", "value": {"app": "test"}}
			]`,
			expectedOps:    1,
			expectFiltered: false,
		},
		{
			name: "replace operation not filtered",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"replicas": 1,
				},
			},
			patches: `[
				{"op": "replace", "path": "/spec/replicas", "value": 3}
			]`,
			expectedOps:    1,
			expectFiltered: false,
		},
		{
			name: "remove from existing array element",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "nginx",
							"image": "nginx:latest",
						},
					},
				},
			},
			patches: `[
				{"op": "remove", "path": "/spec/containers/0/image"}
			]`,
			expectedOps:    1,
			expectFiltered: false,
		},
		{
			name: "remove from non-existent array element",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name": "nginx",
						},
					},
				},
			},
			patches: `[
				{"op": "remove", "path": "/spec/containers/5/image"}
			]`,
			expectedOps:    0,
			expectFiltered: true,
		},
		{
			name: "remove from array element with non-existent field",
			resource: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name": "nginx",
						},
					},
				},
			},
			patches: `[
				{"op": "remove", "path": "/spec/containers/0/image"}
			]`,
			expectedOps:    0,
			expectFiltered: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create unstructured resource
			resource := unstructured.Unstructured{Object: tt.resource}

			// Decode patches
			patches, err := jsonpatch.DecodePatch([]byte(tt.patches))
			if err != nil {
				t.Fatalf("Failed to decode patches: %v", err)
			}

			// Filter patches
			filtered, err := filterValidRemoveOps(resource, patches)
			if err != nil {
				t.Fatalf("filterValidRemoveOps failed: %v", err)
			}

			// Check number of operations
			if len(filtered) != tt.expectedOps {
				t.Errorf("Expected %d operations, got %d", tt.expectedOps, len(filtered))
			}

			// Check if filtering occurred
			wasFiltered := len(filtered) < len(patches)
			if wasFiltered != tt.expectFiltered {
				t.Errorf("Expected filtering=%v, got filtering=%v", tt.expectFiltered, wasFiltered)
			}
		})
	}
}

func TestPathExists(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		path     string
		expected bool
	}{
		{
			name:     "root path",
			data:     map[string]interface{}{"key": "value"},
			path:     "/",
			expected: true,
		},
		{
			name:     "simple existing path",
			data:     map[string]interface{}{"metadata": map[string]interface{}{"uid": "123"}},
			path:     "/metadata/uid",
			expected: true,
		},
		{
			name:     "simple non-existent path",
			data:     map[string]interface{}{"metadata": map[string]interface{}{"name": "test"}},
			path:     "/metadata/uid",
			expected: false,
		},
		{
			name:     "nested existing path",
			data:     map[string]interface{}{"spec": map[string]interface{}{"template": map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "test"}}}}},
			path:     "/spec/template/metadata/labels/app",
			expected: true,
		},
		{
			name:     "nested non-existent path",
			data:     map[string]interface{}{"spec": map[string]interface{}{"replicas": 1}},
			path:     "/spec/template/metadata/labels",
			expected: false,
		},
		{
			name:     "path with escaped slash",
			data:     map[string]interface{}{"metadata": map[string]interface{}{"annotations": map[string]interface{}{"example.com/key": "value"}}},
			path:     "/metadata/annotations/example.com~1key",
			expected: true,
		},
		{
			name:     "array index - first element",
			data:     map[string]interface{}{"spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "nginx"}, map[string]interface{}{"name": "sidecar"}}}},
			path:     "/spec/containers/0",
			expected: true,
		},
		{
			name:     "array index - second element",
			data:     map[string]interface{}{"spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "nginx"}, map[string]interface{}{"name": "sidecar"}}}},
			path:     "/spec/containers/1",
			expected: true,
		},
		{
			name:     "array index - out of bounds",
			data:     map[string]interface{}{"spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "nginx"}}}},
			path:     "/spec/containers/5",
			expected: false,
		},
		{
			name:     "array index - nested field",
			data:     map[string]interface{}{"spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "nginx", "image": "nginx:latest"}}}},
			path:     "/spec/containers/0/image",
			expected: true,
		},
		{
			name:     "array index - non-existent nested field",
			data:     map[string]interface{}{"spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "nginx"}}}},
			path:     "/spec/containers/0/image",
			expected: false,
		},
		{
			name:     "array index - invalid (non-numeric)",
			data:     map[string]interface{}{"spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "nginx"}}}},
			path:     "/spec/containers/first",
			expected: false,
		},
		{
			name:     "array index - leading zero (invalid per RFC 6901)",
			data:     map[string]interface{}{"spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "nginx"}, map[string]interface{}{"name": "sidecar"}}}},
			path:     "/spec/containers/01",
			expected: false,
		},
		{
			name:     "array index - zero is valid",
			data:     map[string]interface{}{"spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "nginx"}}}},
			path:     "/spec/containers/0",
			expected: true,
		},
		{
			name:     "array index - empty segment",
			data:     map[string]interface{}{"spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "nginx"}}}},
			path:     "/spec/containers//name",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pathExists(tt.data, tt.path)
			if result != tt.expected {
				t.Errorf("pathExists(%v, %q) = %v, want %v", tt.data, tt.path, result, tt.expected)
			}
		})
	}
}
