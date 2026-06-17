package transform

import (
	"encoding/json"
	"testing"

	jsonpatch "github.com/evanphx/json-patch"
	cranelib "github.com/konveyor/crane-lib/transform"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// mockPluginWithOptionals implements cranelib.Plugin and tracks whether optionals were received
type mockPluginWithOptionals struct {
	name              string
	receivedOptionals map[string]string
}

func (m *mockPluginWithOptionals) Run(request cranelib.PluginRequest) (cranelib.PluginResponse, error) {
	// Capture the optionals that were passed to the plugin
	m.receivedOptionals = request.Extras

	// Return a simple response with no transformations (empty patch)
	return cranelib.PluginResponse{
		Version:    string(cranelib.V1),
		Patches:    jsonpatch.Patch{},
		IsWhiteOut: false,
	}, nil
}

func (m *mockPluginWithOptionals) Metadata() cranelib.PluginMetadata {
	return cranelib.PluginMetadata{
		Name:            m.name,
		Version:         "test",
		RequestVersion:  []cranelib.Version{cranelib.V1},
		ResponseVersion: []cranelib.Version{cranelib.V1},
	}
}

// TestOptionalFlags_PassedToPlugin verifies that optional flags are passed to plugins via Runner
func TestOptionalFlags_PassedToPlugin(t *testing.T) {
	// Setup test optionals
	testOptionals := map[string]string{
		"registry-replacement": "docker.io=quay.io",
		"add-annotations":      "migration=crane,team=platform",
	}

	// Create mock plugin
	mockPlugin := &mockPluginWithOptionals{
		name:              "TestPlugin",
		receivedOptionals: make(map[string]string),
	}

	// Create a simple test resource
	testResource := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test-configmap",
				"namespace": "default",
			},
			"data": map[string]interface{}{
				"key": "value",
			},
		},
	}

	log := logrus.New()
	log.SetLevel(logrus.FatalLevel) // Suppress log output during tests

	// Create runner with optionals - this is what orchestrator uses
	runner := cranelib.Runner{
		Log:           log,
		OptionalFlags: testOptionals,
	}

	// Run the plugin - this should pass optionals
	_, err := runner.Run(testResource, []cranelib.Plugin{mockPlugin})
	if err != nil {
		t.Fatalf("failed to run plugin: %v", err)
	}

	// Verify that optionals were passed to the plugin
	if len(mockPlugin.receivedOptionals) == 0 {
		t.Fatal("expected plugin to receive optionals, but got none")
	}

	for key, expectedValue := range testOptionals {
		receivedValue, ok := mockPlugin.receivedOptionals[key]
		if !ok {
			t.Errorf("expected plugin to receive optional key %q, but it was missing", key)
			continue
		}
		if receivedValue != expectedValue {
			t.Errorf("optional key %q: expected value %q, got %q", key, expectedValue, receivedValue)
		}
	}
}

// TestOptionalFlagsToLower verifies that optional flags keys are lowercased
func TestOptionalFlagsToLower(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected map[string]string
	}{
		{
			name: "mixed case keys",
			input: map[string]string{
				"Registry-Replacement": "docker.io=quay.io",
				"Add-Annotations":      "migration=crane",
			},
			expected: map[string]string{
				"registry-replacement": "docker.io=quay.io",
				"add-annotations":      "migration=crane",
			},
		},
		{
			name: "already lowercase",
			input: map[string]string{
				"registry-replacement": "docker.io=quay.io",
			},
			expected: map[string]string{
				"registry-replacement": "docker.io=quay.io",
			},
		},
		{
			name:     "empty map",
			input:    map[string]string{},
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := optionalFlagsToLower(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d keys, got %d", len(tt.expected), len(result))
			}

			for key, expectedValue := range tt.expected {
				resultValue, ok := result[key]
				if !ok {
					t.Errorf("expected key %q to be present in result", key)
					continue
				}
				if resultValue != expectedValue {
					t.Errorf("key %q: expected value %q, got %q", key, expectedValue, resultValue)
				}
			}
		})
	}
}

// TestOptionalFlags_JSONParsing verifies that optional flags JSON string is parsed correctly
func TestOptionalFlags_JSONParsing(t *testing.T) {
	tests := []struct {
		name        string
		jsonString  string
		expected    map[string]string
		expectError bool
	}{
		{
			name:       "valid JSON",
			jsonString: `{"registry-replacement": "docker.io=quay.io", "add-annotations": "migration=crane"}`,
			expected: map[string]string{
				"registry-replacement": "docker.io=quay.io",
				"add-annotations":      "migration=crane",
			},
			expectError: false,
		},
		{
			name:       "empty JSON object",
			jsonString: `{}`,
			expected:   map[string]string{},
			expectError: false,
		},
		{
			name:        "invalid JSON",
			jsonString:  `{invalid json}`,
			expectError: true,
		},
		{
			name:       "JSON with special characters",
			jsonString: `{"pvc-rename-map": "old-pvc-1:new-pvc-1,old-pvc-2:new-pvc-2"}`,
			expected: map[string]string{
				"pvc-rename-map": "old-pvc-1:new-pvc-1,old-pvc-2:new-pvc-2",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result map[string]string
			err := json.Unmarshal([]byte(tt.jsonString), &result)

			if tt.expectError {
				if err == nil {
					t.Error("expected error parsing JSON, but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error parsing JSON: %v", err)
			}

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d keys, got %d", len(tt.expected), len(result))
			}

			for key, expectedValue := range tt.expected {
				resultValue, ok := result[key]
				if !ok {
					t.Errorf("expected key %q to be present in result", key)
					continue
				}
				if resultValue != expectedValue {
					t.Errorf("key %q: expected value %q, got %q", key, expectedValue, resultValue)
				}
			}
		})
	}
}
