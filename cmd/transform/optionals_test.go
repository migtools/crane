package transform

import (
	"testing"

	jsonpatch "github.com/evanphx/json-patch"
	cranelib "github.com/konveyor/crane-lib/transform"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// mockPluginWithOptionals implements cranelib.Plugin and captures received optionals
type mockPluginWithOptionals struct {
	name              string
	receivedOptionals map[string]string
}

func (m *mockPluginWithOptionals) Run(request cranelib.PluginRequest) (cranelib.PluginResponse, error) {
	m.receivedOptionals = request.Extras
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

// testConfigMap creates a simple ConfigMap for testing
func testConfigMap() unstructured.Unstructured {
	return unstructured.Unstructured{
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
}

// assertMapEquals verifies two string maps are equal
func assertMapEquals(t *testing.T, expected, actual map[string]string) {
	t.Helper()

	if len(actual) != len(expected) {
		t.Errorf("expected %d keys, got %d", len(expected), len(actual))
	}

	for key, expectedValue := range expected {
		actualValue, ok := actual[key]
		if !ok {
			t.Errorf("expected key %q to be present", key)
			continue
		}
		if actualValue != expectedValue {
			t.Errorf("key %q: expected value %q, got %q", key, expectedValue, actualValue)
		}
	}
}

// TestOptionalFlags_PassedToPlugin verifies that optional flags are passed to plugins via Runner
func TestOptionalFlags_PassedToPlugin(t *testing.T) {
	testOptionals := map[string]string{
		"registry-replacement": "docker.io=quay.io",
		"add-annotations":      "migration=crane,team=platform",
	}

	mockPlugin := &mockPluginWithOptionals{
		name:              "TestPlugin",
		receivedOptionals: make(map[string]string),
	}

	log := logrus.New()
	log.SetLevel(logrus.FatalLevel)

	runner := cranelib.Runner{
		Log:           log,
		OptionalFlags: testOptionals,
	}

	_, err := runner.Run(testConfigMap(), []cranelib.Plugin{mockPlugin})
	if err != nil {
		t.Fatalf("failed to run plugin: %v", err)
	}

	if len(mockPlugin.receivedOptionals) == 0 {
		t.Fatal("expected plugin to receive optionals, but got none")
	}

	assertMapEquals(t, testOptionals, mockPlugin.receivedOptionals)
}

// TestOptionalFlagsToLower verifies that optional flags keys are normalized to lowercase
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
		{
			name: "uppercase keys",
			input: map[string]string{
				"FOO": "bar",
				"BAZ": "qux",
			},
			expected: map[string]string{
				"foo": "bar",
				"baz": "qux",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := optionalFlagsToLower(tt.input)
			assertMapEquals(t, tt.expected, result)
		})
	}
}
