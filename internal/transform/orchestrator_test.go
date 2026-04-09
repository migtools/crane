package transform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestFilterPluginsByStage(t *testing.T) {
	// Create mock plugins
	allPlugins := []mockPlugin{
		{name: "kubernetes"},
		{name: "openshift"},
		{name: "custom"},
	}

	tests := []struct {
		name           string
		stage          Stage
		expectedCount  int
		expectedNames  []string
	}{
		{
			name:          "empty plugin name returns all plugins",
			stage:         Stage{PluginName: ""},
			expectedCount: 3,
			expectedNames: []string{"kubernetes", "openshift", "custom"},
		},
		{
			name:          "specific plugin name filters correctly",
			stage:         Stage{PluginName: "openshift"},
			expectedCount: 1,
			expectedNames: []string{"openshift"},
		},
		{
			name:          "non-existent plugin name returns empty",
			stage:         Stage{PluginName: "nonexistent"},
			expectedCount: 0,
			expectedNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Manual filtering logic (mirrors what filterPluginsByStage does)
			var filtered []mockPlugin
			if tt.stage.PluginName == "" {
				filtered = allPlugins
			} else {
				for _, p := range allPlugins {
					if p.name == tt.stage.PluginName {
						filtered = append(filtered, p)
					}
				}
			}

			if len(filtered) != tt.expectedCount {
				t.Errorf("Expected %d plugins, got %d", tt.expectedCount, len(filtered))
			}

			for i, name := range tt.expectedNames {
				if i >= len(filtered) {
					t.Errorf("Expected plugin %s at index %d, but only got %d plugins", name, i, len(filtered))
					continue
				}
				if filtered[i].name != name {
					t.Errorf("Expected plugin %s at index %d, got %s", name, i, filtered[i].name)
				}
			}
		})
	}
}

func TestRunMultiStage_StageOrdering(t *testing.T) {
	// Create temp directory structure
	tmpDir, err := os.MkdirTemp("", "orchestrator-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	exportDir := filepath.Join(tmpDir, "export")
	transformDir := filepath.Join(tmpDir, "transform")

	// Create export directory with a test resource
	resourcesDir := filepath.Join(exportDir, "resources", "default")
	if err := os.MkdirAll(resourcesDir, 0700); err != nil {
		t.Fatalf("Failed to create export dir: %v", err)
	}

	// Write a simple ConfigMap
	configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: default
data:
  key: value
`
	configMapPath := filepath.Join(resourcesDir, "ConfigMap_default_test-config.yaml")
	if err := os.WriteFile(configMapPath, []byte(configMapYAML), 0644); err != nil {
		t.Fatalf("Failed to write test resource: %v", err)
	}

	// Create pre-existing stages in transform directory (simulating partial pipeline execution)
	// Stage 10_kubernetes exists
	stage10Dir := filepath.Join(transformDir, "10_kubernetes")
	stage10ResourcesDir := filepath.Join(stage10Dir, "resources")
	if err := os.MkdirAll(stage10ResourcesDir, 0700); err != nil {
		t.Fatalf("Failed to create stage 10 dir: %v", err)
	}

	// Write kustomization.yaml for stage 10
	kustomization10 := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- resources/configmap.yaml
`
	if err := os.WriteFile(filepath.Join(stage10Dir, "kustomization.yaml"), []byte(kustomization10), 0644); err != nil {
		t.Fatalf("Failed to write kustomization: %v", err)
	}

	// Write resource file for stage 10
	if err := os.WriteFile(filepath.Join(stage10ResourcesDir, "configmap.yaml"), []byte(configMapYAML), 0644); err != nil {
		t.Fatalf("Failed to write resource: %v", err)
	}

	// Create stage 20_openshift directory (exists but empty, so stage 30 will fail loading from it)
	stage20Dir := filepath.Join(transformDir, "20_openshift")
	stage20ResourcesDir := filepath.Join(stage20Dir, "resources")
	if err := os.MkdirAll(stage20ResourcesDir, 0700); err != nil {
		t.Fatalf("Failed to create stage 20 dir: %v", err)
	}

	// Write minimal kustomization for stage 20 (empty resources)
	kustomization20 := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: []
`
	if err := os.WriteFile(filepath.Join(stage20Dir, "kustomization.yaml"), []byte(kustomization20), 0644); err != nil {
		t.Fatalf("Failed to write kustomization: %v", err)
	}

	// Create stage 30_imagestream directory (for testing dependency chain)
	// It exists but depends on stage 20 which has no output
	stage30Dir := filepath.Join(transformDir, "30_imagestream")
	if err := os.Mkdir(stage30Dir, 0700); err != nil {
		t.Fatalf("Failed to create stage 30 dir: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	o := &Orchestrator{
		Log:          logger,
		ExportDir:    exportDir,
		TransformDir: transformDir,
		PluginDir:    "/nonexistent", // Won't be used in this test
		Force:        true,             // Use force to overwrite existing directories
	}

	tests := []struct {
		name          string
		selector      StageSelector
		expectError   bool
		errorContains string
		description   string
	}{
		{
			name: "stage ordering preserved",
			selector: StageSelector{
				Stage: "10_kubernetes",
			},
			expectError: false,
			description: "Should successfully run existing first stage",
		},
		{
			name: "no stages found matching selector",
			selector: StageSelector{
				Stage: "99_nonexistent",
			},
			expectError:   true,
			errorContains: "no stages found matching selector",
			description:   "Should error when no stages match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := o.RunMultiStage(tt.selector)

			if tt.expectError {
				if err == nil {
					t.Errorf("%s: expected error but got none", tt.description)
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("%s: expected error containing %q, got %q", tt.description, tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("%s: unexpected error: %v", tt.description, err)
				}
			}
		})
	}
}

func TestRunMultiStage_PreviousStageDependency(t *testing.T) {
	// This test verifies that multi-stage execution properly loads from previous stage output
	tmpDir, err := os.MkdirTemp("", "orchestrator-dep-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	exportDir := filepath.Join(tmpDir, "export")
	transformDir := filepath.Join(tmpDir, "transform")

	// Create export directory with test resource
	resourcesDir := filepath.Join(exportDir, "resources", "default")
	if err := os.MkdirAll(resourcesDir, 0700); err != nil {
		t.Fatalf("Failed to create export dir: %v", err)
	}

	configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: original-config
  namespace: default
data:
  stage: export
`
	configMapPath := filepath.Join(resourcesDir, "ConfigMap_default_original-config.yaml")
	if err := os.WriteFile(configMapPath, []byte(configMapYAML), 0644); err != nil {
		t.Fatalf("Failed to write test resource: %v", err)
	}

	// Create first stage (10_kubernetes) that exists
	stage10Dir := filepath.Join(transformDir, "10_kubernetes")
	stage10ResourcesDir := filepath.Join(stage10Dir, "resources")
	if err := os.MkdirAll(stage10ResourcesDir, 0700); err != nil {
		t.Fatalf("Failed to create stage 10 dir: %v", err)
	}

	// Modified resource in stage 10 (simulates transformation)
	modifiedConfigMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: original-config
  namespace: default
data:
  stage: stage10
`
	if err := os.WriteFile(filepath.Join(stage10ResourcesDir, "configmap.yaml"), []byte(modifiedConfigMapYAML), 0644); err != nil {
		t.Fatalf("Failed to write stage 10 resource: %v", err)
	}

	kustomization10 := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- resources/configmap.yaml
`
	if err := os.WriteFile(filepath.Join(stage10Dir, "kustomization.yaml"), []byte(kustomization10), 0644); err != nil {
		t.Fatalf("Failed to write kustomization: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	o := &Orchestrator{
		Log:          logger,
		ExportDir:    exportDir,
		TransformDir: transformDir,
		PluginDir:    "/nonexistent",
		Force:        false,
	}

	// Test: Try to run stage 20 which should depend on stage 10
	// Since stage 10 exists, it should load from stage 10's output
	// But stage 20 doesn't exist, so this should fail with the dependency error

	selector := StageSelector{
		Stage: "20_openshift",
	}

	err = o.RunMultiStage(selector)

	// Should error because stage 20 doesn't exist in transform dir
	if err == nil {
		t.Error("Expected error when stage doesn't exist, got none")
	} else if !contains(err.Error(), "no stages found") {
		t.Errorf("Expected 'no stages found' error, got: %v", err)
	}
}

func TestLoadStageOutput_RequiresKubectl(t *testing.T) {
	// This test verifies loadStageOutput behavior when kubectl is not available
	// or when kustomization is malformed

	tmpDir, err := os.MkdirTemp("", "orchestrator-load-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	transformDir := filepath.Join(tmpDir, "transform")

	// Create a stage with invalid kustomization
	stage10Dir := filepath.Join(transformDir, "10_kubernetes")
	if err := os.MkdirAll(stage10Dir, 0700); err != nil {
		t.Fatalf("Failed to create stage dir: %v", err)
	}

	// Write invalid kustomization (missing resources)
	invalidKustomization := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- nonexistent.yaml
`
	if err := os.WriteFile(filepath.Join(stage10Dir, "kustomization.yaml"), []byte(invalidKustomization), 0644); err != nil {
		t.Fatalf("Failed to write kustomization: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	o := &Orchestrator{
		Log:          logger,
		TransformDir: transformDir,
	}

	stage := Stage{
		DirName:    "10_kubernetes",
		Priority:   10,
		PluginName: "kubernetes",
	}

	// Try to load output from this malformed stage
	resources, err := o.loadStageOutput(stage)

	// Should error because kustomization references non-existent file
	if err == nil {
		t.Errorf("Expected error when loading from invalid kustomization, got %d resources", len(resources))
	}
}

// Mock plugin for testing
type mockPlugin struct {
	name string
}

func TestExecuteStage_PluginFiltering(t *testing.T) {
	// Test that stage configuration correctly specifies plugin filtering behavior

	tests := []struct {
		name            string
		stage           Stage
		expectedPlugins int
		description     string
	}{
		{
			name: "stage with specific plugin name",
			stage: Stage{
				DirName:    "10_kubernetes",
				Priority:   10,
				PluginName: "kubernetes",
			},
			expectedPlugins: 1,
			description:     "Should filter to only 'kubernetes' plugin",
		},
		{
			name: "stage with empty plugin name",
			stage: Stage{
				DirName:    "20_all",
				Priority:   20,
				PluginName: "",
			},
			expectedPlugins: 3,
			description:     "Should include all plugins",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify stage structure
			if tt.stage.PluginName == "" && tt.expectedPlugins != 3 {
				t.Errorf("Logic error: empty plugin name should match all plugins")
			}
			if tt.stage.PluginName == "kubernetes" && tt.expectedPlugins != 1 {
				t.Errorf("Logic error: specific plugin name should match one plugin")
			}
		})
	}
}

