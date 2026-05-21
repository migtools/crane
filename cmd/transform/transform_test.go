package transform

import (
	"os"
	"path/filepath"
	"testing"

	cranelib "github.com/konveyor/crane-lib/transform"
	internalTransform "github.com/konveyor/crane/internal/transform"
	"github.com/sirupsen/logrus"
)

func TestSanitizePluginName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "already sanitized",
			input:    "KubernetesPlugin",
			expected: "KubernetesPlugin",
		},
		{
			name:     "hyphenated name",
			input:    "namespace-cleanup",
			expected: "NamespaceCleanupPlugin",
		},
		{
			name:     "underscored name",
			input:    "security_scanner",
			expected: "SecurityScannerPlugin",
		},
		{
			name:     "mixed separators",
			input:    "my-custom_plugin",
			expected: "MyCustomPlugin",
		},
		{
			name:     "single word lowercase",
			input:    "helm",
			expected: "HelmPlugin",
		},
		{
			name:     "multiple hyphens",
			input:    "foo-bar-baz",
			expected: "FooBarBazPlugin",
		},
		{
			name:     "already has Plugin suffix",
			input:    "CustomPlugin",
			expected: "CustomPlugin",
		},
		{
			name:     "path traversal with slashes",
			input:    "../../../etc/passwd",
			expected: "EtcPasswdPlugin",
		},
		{
			name:     "path traversal with backslashes",
			input:    "..\\..\\windows\\system32",
			expected: "WindowsSystem32Plugin",
		},
		{
			name:     "mixed path separators",
			input:    "../path/to/../secret",
			expected: "PathToSecretPlugin",
		},
		{
			name:     "dots only",
			input:    "...",
			expected: "Plugin",
		},
		{
			name:     "special characters filtered",
			input:    "test@plugin#name!",
			expected: "TestPluginNamePlugin",
		},
		{
			name:     "empty after sanitization",
			input:    "@#$%^&*()",
			expected: "Plugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizePluginName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizePluginName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// mockPlugin implements cranelib.Plugin for testing
type mockPlugin struct {
	name string
}

func (m *mockPlugin) Run(request cranelib.PluginRequest) (cranelib.PluginResponse, error) {
	return cranelib.PluginResponse{}, nil
}

func (m *mockPlugin) Metadata() cranelib.PluginMetadata {
	return cranelib.PluginMetadata{
		Name:            m.name,
		Version:         "test",
		RequestVersion:  []cranelib.Version{cranelib.V1},
		ResponseVersion: []cranelib.Version{cranelib.V1},
	}
}

func TestCreateDefaultStagesForAllPlugins(t *testing.T) {
	tests := []struct {
		name           string
		pluginNames    []string
		expectedStages []string
		expectedError  bool
	}{
		{
			name:        "single plugin",
			pluginNames: []string{"KubernetesPlugin"},
			expectedStages: []string{
				"10_KubernetesPlugin",
			},
			expectedError: false,
		},
		{
			name:        "multiple plugins alphabetically ordered",
			pluginNames: []string{"ZebraPlugin", "AlphaPlugin", "BetaPlugin"},
			expectedStages: []string{
				"10_AlphaPlugin",
				"15_BetaPlugin",
				"20_ZebraPlugin",
			},
			expectedError: false,
		},
		{
			name:        "plugins with hyphens",
			pluginNames: []string{"namespace-cleanup", "security-scanner"},
			expectedStages: []string{
				"10_NamespaceCleanupPlugin",
				"15_SecurityScannerPlugin",
			},
			expectedError: false,
		},
		{
			name:           "no plugins",
			pluginNames:    []string{},
			expectedStages: []string{},
			expectedError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary transform directory
			tmpDir, err := os.MkdirTemp("", "crane-transform-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			// Create mock plugins
			var plugins []cranelib.Plugin
			for _, name := range tt.pluginNames {
				plugins = append(plugins, &mockPlugin{name: name})
			}

			// Setup options and orchestrator
			opts := &Options{}
			log := logrus.New()
			log.SetLevel(logrus.FatalLevel) // Suppress log output during tests

			orchestrator := &internalTransform.Orchestrator{
				NewlyCreatedStages: make(map[string]bool),
			}

			// Call the function
			stageNames, err := opts.createDefaultStagesForAllPlugins(orchestrator, tmpDir, plugins, log)

			// Check error
			if tt.expectedError && err == nil {
				t.Errorf("Expected error but got nil")
			}
			if !tt.expectedError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Check stage names
			if len(stageNames) != len(tt.expectedStages) {
				t.Errorf("Expected %d stages, got %d", len(tt.expectedStages), len(stageNames))
			}

			for i, expected := range tt.expectedStages {
				if i >= len(stageNames) {
					break
				}
				if stageNames[i] != expected {
					t.Errorf("Stage %d: expected %q, got %q", i, expected, stageNames[i])
				}

				// Verify directory was created
				stageDir := filepath.Join(tmpDir, expected)
				if _, err := os.Stat(stageDir); os.IsNotExist(err) {
					t.Errorf("Stage directory %q was not created", stageDir)
				}

				// Verify stage was marked as newly created
				if !orchestrator.NewlyCreatedStages[expected] {
					t.Errorf("Stage %q was not marked as newly created", expected)
				}
			}
		})
	}
}

func TestCreateDefaultStagesForAllPlugins_Priority(t *testing.T) {
	// Test that priority increments by 5
	tmpDir, err := os.MkdirTemp("", "crane-transform-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	plugins := []cranelib.Plugin{
		&mockPlugin{name: "First"},
		&mockPlugin{name: "Second"},
		&mockPlugin{name: "Third"},
	}

	opts := &Options{}
	log := logrus.New()
	log.SetLevel(logrus.FatalLevel)

	orchestrator := &internalTransform.Orchestrator{
		NewlyCreatedStages: make(map[string]bool),
	}

	stageNames, err := opts.createDefaultStagesForAllPlugins(orchestrator, tmpDir, plugins, log)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expected := []string{"10_FirstPlugin", "15_SecondPlugin", "20_ThirdPlugin"}
	for i, exp := range expected {
		if stageNames[i] != exp {
			t.Errorf("Stage %d: expected %q, got %q", i, exp, stageNames[i])
		}
	}
}
