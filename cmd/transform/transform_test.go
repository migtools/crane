package transform

import (
	"os"
	"path/filepath"
	"testing"

	cranelib "github.com/konveyor/crane-lib/transform"
	internalTransform "github.com/konveyor/crane/internal/transform"
	"github.com/sirupsen/logrus"
)

func TestValidatePluginNameForStage(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{
			name:      "valid CamelCase name",
			input:     "KubernetesPlugin",
			expectErr: false,
		},
		{
			name:      "valid hyphenated name",
			input:     "namespace-cleanup",
			expectErr: false,
		},
		{
			name:      "valid underscored name",
			input:     "security_scanner",
			expectErr: false,
		},
		{
			name:      "valid mixed separators",
			input:     "my-custom_plugin",
			expectErr: false,
		},
		{
			name:      "valid single word",
			input:     "helm",
			expectErr: false,
		},
		{
			name:      "invalid: forward slash",
			input:     "../../../etc/passwd",
			expectErr: true,
		},
		{
			name:      "invalid: backslash",
			input:     "..\\..\\windows",
			expectErr: true,
		},
		{
			name:      "invalid: path traversal",
			input:     "../secret",
			expectErr: true,
		},
		{
			name:      "invalid: dot traversal",
			input:     "...",
			expectErr: true,
		},
		{
			name:      "invalid: single dot",
			input:     ".",
			expectErr: true,
		},
		{
			name:      "invalid: special characters",
			input:     "test@plugin#name!",
			expectErr: true,
		},
		{
			name:      "invalid: empty",
			input:     "",
			expectErr: true,
		},
		{
			name:      "invalid: only special chars",
			input:     "@#$%^&*()",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePluginNameForStage(tt.input)
			if tt.expectErr && err == nil {
				t.Errorf("validatePluginNameForStage(%q) expected error but got nil", tt.input)
			}
			if !tt.expectErr && err != nil {
				t.Errorf("validatePluginNameForStage(%q) unexpected error: %v", tt.input, err)
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
			name:        "plugins with hyphens and Plugin suffix",
			pluginNames: []string{"namespace-cleanupPlugin", "security-scannerPlugin"},
			expectedStages: []string{
				"10_namespace-cleanupPlugin",
				"15_security-scannerPlugin",
			},
			expectedError: false,
		},
		{
			name:           "plugins without Plugin suffix are skipped",
			pluginNames:    []string{"namespace-cleanup", "helm"},
			expectedStages: []string{}, // All skipped
			expectedError:  false,
		},
		{
			name:           "unsafe plugin names are skipped",
			pluginNames:    []string{"../../../etc/passwd", "KubernetesPlugin"},
			expectedStages: []string{"10_KubernetesPlugin"}, // Only safe one created
			expectedError:  false,
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
		&mockPlugin{name: "FirstPlugin"},
		&mockPlugin{name: "SecondPlugin"},
		&mockPlugin{name: "ThirdPlugin"},
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

func TestCreateDefaultStagesForAllPlugins_PluginNameResolution(t *testing.T) {
	// Test that plugin with hyphenated name like "namespace-cleanupPlugin"
	// creates stage "10_namespace-cleanupPlugin" and can be resolved back correctly
	tmpDir, err := os.MkdirTemp("", "crane-transform-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create plugins with Plugin suffix
	plugins := []cranelib.Plugin{
		&mockPlugin{name: "namespace-cleanupPlugin"},
		&mockPlugin{name: "KubernetesPlugin"},
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

	// Verify exact stage names
	expectedStages := []string{"10_KubernetesPlugin", "15_namespace-cleanupPlugin"}
	if len(stageNames) != len(expectedStages) {
		t.Fatalf("Expected %d stages, got %d", len(expectedStages), len(stageNames))
	}

	for i, expected := range expectedStages {
		if stageNames[i] != expected {
			t.Errorf("Stage %d: expected %q, got %q", i, expected, stageNames[i])
		}

		// Verify directory exists
		stageDir := filepath.Join(tmpDir, expected)
		if _, err := os.Stat(stageDir); os.IsNotExist(err) {
			t.Errorf("Stage directory %q was not created", stageDir)
		}
	}

	// Now test that stage discovery can parse these back correctly
	discoveredStages, err := internalTransform.DiscoverStages(tmpDir)
	if err != nil {
		t.Fatalf("Failed to discover stages: %v", err)
	}

	if len(discoveredStages) != 2 {
		t.Fatalf("Expected 2 discovered stages, got %d", len(discoveredStages))
	}

	// Verify plugin names match exactly
	if discoveredStages[0].PluginName != "KubernetesPlugin" {
		t.Errorf("Stage 0 plugin name: expected %q, got %q", "KubernetesPlugin", discoveredStages[0].PluginName)
	}
	if discoveredStages[1].PluginName != "namespace-cleanupPlugin" {
		t.Errorf("Stage 1 plugin name: expected %q, got %q", "namespace-cleanupPlugin", discoveredStages[1].PluginName)
	}
}
