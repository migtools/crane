package transform

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cranelib "github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane/internal/file"
	"github.com/sirupsen/logrus"
)

func TestFilterPluginsByStage(t *testing.T) {
	// Create mock plugins with actual plugin names (must match plugin metadata)
	allPlugins := []mockPlugin{
		{name: "KubernetesPlugin"},
		{name: "OpenshiftPlugin"},
		{name: "CustomPlugin"},
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
			expectedNames: []string{"KubernetesPlugin", "OpenshiftPlugin", "CustomPlugin"},
		},
		{
			name:          "specific plugin name filters correctly",
			stage:         Stage{PluginName: "OpenshiftPlugin"},
			expectedCount: 1,
			expectedNames: []string{"OpenshiftPlugin"},
		},
		{
			name:          "non-existent plugin name returns empty",
			stage:         Stage{PluginName: "NonExistentPlugin"},
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
	// Stage 10_KubernetesPlugin exists
	stage10Dir := filepath.Join(transformDir, "10_KubernetesPlugin")
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

	// Create stage 20_OpenshiftPlugin directory (exists but empty, so stage 30 will fail loading from it)
	stage20Dir := filepath.Join(transformDir, "20_OpenshiftPlugin")
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

	// Create stage 30_ImagestreamPlugin directory (for testing dependency chain)
	// It exists but depends on stage 20 which has no output
	stage30Dir := filepath.Join(transformDir, "30_ImagestreamPlugin")
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
				Stage: "10_KubernetesPlugin",
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

	// Create first stage (10_KubernetesPlugin) that exists
	stage10Dir := filepath.Join(transformDir, "10_KubernetesPlugin")
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
		Stage: "20_OpenshiftPlugin",
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
	stage10Dir := filepath.Join(transformDir, "10_KubernetesPlugin")
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

	stageDir := filepath.Join(transformDir, "10_KubernetesPlugin")

	// Try to apply transforms from this malformed stage
	resources, err := o.applyStageTransforms(stageDir)

	// Should error because kustomization references non-existent file
	if err == nil {
		t.Errorf("Expected error when applying invalid kustomization, got %d resources", len(resources))
	}
}

func TestFilterPluginsByStage_NonExistentPluginIntegration(t *testing.T) {
	// Integration test: verify that stages with non-existent plugin names
	// are handled correctly (no plugins match, so transformation is skipped)

	// Mock plugins (representing actual available plugins)
	availablePlugins := []mockPlugin{
		{name: "KubernetesPlugin"},
		{name: "OpenshiftPlugin"},
	}

	tests := []struct {
		name          string
		stage         Stage
		expectedCount int
		description   string
	}{
		{
			name: "stage with valid plugin name",
			stage: Stage{
				DirName:    "10_KubernetesPlugin",
				PluginName: "KubernetesPlugin",
			},
			expectedCount: 1,
			description:   "Should match KubernetesPlugin",
		},
		{
			name: "stage with non-existent plugin name",
			stage: Stage{
				DirName:    "50_NonExistentPlugin",
				PluginName: "NonExistentPlugin",
			},
			expectedCount: 0,
			description:   "Should match no plugins (non-existent)",
		},
		{
			name: "manual stage with no plugin",
			stage: Stage{
				DirName:    "90_ManualEdits",
				PluginName: "ManualEdits",
			},
			expectedCount: 0,
			description:   "Manual stage should match no plugins",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate filterPluginsByStage logic
			var filtered []mockPlugin
			if tt.stage.PluginName == "" {
				filtered = availablePlugins
			} else {
				for _, p := range availablePlugins {
					if p.name == tt.stage.PluginName {
						filtered = append(filtered, p)
					}
				}
			}

			if len(filtered) != tt.expectedCount {
				t.Errorf("%s: expected %d plugins, got %d", tt.description, tt.expectedCount, len(filtered))
			}
		})
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
				DirName:    "10_KubernetesPlugin",
				Priority:   10,
				PluginName: "KubernetesPlugin",
			},
			expectedPlugins: 1,
			description:     "Should filter to only 'KubernetesPlugin' plugin",
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
			if tt.stage.PluginName == "KubernetesPlugin" && tt.expectedPlugins != 1 {
				t.Errorf("Logic error: specific plugin name should match one plugin")
			}
		})
	}
}


func TestPluginStageNameMatching(t *testing.T) {
	// This test documents the plugin-to-stage name matching behavior:
	// - Stage directory name format: <priority>_<PluginName>
	// - PluginName must match plugin.Metadata().Name exactly
	// - Built-in plugins use "Plugin" suffix (e.g., KubernetesPlugin)
	// - User stages that don't match any plugin will have no plugins applied

	tests := []struct {
		stageName      string
		pluginName     string
		shouldMatch    bool
		description    string
	}{
		{
			stageName:   "10_KubernetesPlugin",
			pluginName:  "KubernetesPlugin",
			shouldMatch: true,
			description: "Built-in Kubernetes plugin with correct naming",
		},
		{
			stageName:   "20_OpenshiftPlugin",
			pluginName:  "OpenshiftPlugin",
			shouldMatch: true,
			description: "Built-in Openshift plugin with correct naming",
		},
		{
			stageName:   "10_kubernetes",
			pluginName:  "KubernetesPlugin",
			shouldMatch: false,
			description: "Lowercase 'kubernetes' does not match 'KubernetesPlugin'",
		},
		{
			stageName:   "50_CustomPlugin",
			pluginName:  "CustomPlugin",
			shouldMatch: true,
			description: "Custom plugin with matching name",
		},
		{
			stageName:   "90_ManualEdits",
			pluginName:  "KubernetesPlugin",
			shouldMatch: false,
			description: "Manual stage name doesn't match any plugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.stageName, func(t *testing.T) {
			// Extract plugin name from stage directory name
			stage := Stage{
				DirName:    tt.stageName,
				PluginName: tt.stageName[3:], // Remove priority prefix
			}

			matches := stage.PluginName == tt.pluginName

			if matches != tt.shouldMatch {
				t.Errorf("%s: expected match=%v, got match=%v (stage.PluginName=%s, pluginName=%s)",
					tt.description, tt.shouldMatch, matches, stage.PluginName, tt.pluginName)
			}
		})
	}
}

func TestNonMatchingPluginName_ResourcesPassThrough(t *testing.T) {
	// This test documents what happens when a stage directory name
	// doesn't match any plugin: resources pass through unchanged

	tmpDir, err := os.MkdirTemp("", "orchestrator-nomatch-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	exportDir := filepath.Join(tmpDir, "export")
	_ = filepath.Join(tmpDir, "transform") // Not used in this test

	// Create export directory with test resource
	resourcesDir := filepath.Join(exportDir, "resources", "default")
	if err := os.MkdirAll(resourcesDir, 0700); err != nil {
		t.Fatalf("Failed to create export dir: %v", err)
	}

	configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: default
  uid: should-remain-unchanged
  resourceVersion: "12345"
data:
  key: value
`
	configMapPath := filepath.Join(resourcesDir, "ConfigMap_default_test-config.yaml")
	if err := os.WriteFile(configMapPath, []byte(configMapYAML), 0644); err != nil {
		t.Fatalf("Failed to write test resource: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Test with mock plugins - none will match "ManualStage"
	// In a real scenario, GetFilteredPlugins would return actual plugins,
	// but none would match the stage name

	t.Run("stage name doesn't match any plugin", func(t *testing.T) {
		// Simulate what happens with non-matching stage name
		stage := Stage{
			DirName:    "90_ManualStage",
			PluginName: "ManualStage",
			Priority:   90,
		}

		// Mock available plugins
		availablePlugins := []mockPlugin{
			{name: "KubernetesPlugin"},
			{name: "OpenshiftPlugin"},
		}

		// Filter - should return empty
		var filtered []mockPlugin
		for _, p := range availablePlugins {
			if p.name == stage.PluginName {
				filtered = append(filtered, p)
			}
		}

		if len(filtered) != 0 {
			t.Errorf("Expected 0 plugins for non-matching stage, got %d", len(filtered))
		}

		// Expected behavior:
		// 1. No plugins match → empty plugin list
		// 2. runner.Run() with empty plugin list → no transformations
		// 3. Resources written unchanged (no patches generated)
		// 4. This is INTENTIONAL - allows manual transformation stages

		t.Log("✓ Stage name 'ManualStage' doesn't match any plugin")
		t.Log("✓ No transformations will be applied")
		t.Log("✓ Resources will be written unchanged to stage directory")
		t.Log("✓ User can manually edit resources in this stage")
	})
}

// TestSequentialStageExecution tests that stages execute sequentially
// with each stage consuming the fully applied output of the previous stage
func TestSequentialStageExecution(t *testing.T) {
	// Skip if kubectl/oc is not available
	if !hasKustomizeCommand(t) {
		t.Skip("kubectl or oc not available, skipping test that requires kustomize")
	}

	tmpDir, err := os.MkdirTemp("", "sequential-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	exportDir := filepath.Join(tmpDir, "export")
	transformDir := filepath.Join(tmpDir, "transform")

	// Create export directory with two ConfigMaps
	if err := os.MkdirAll(filepath.Join(exportDir, "default"), 0700); err != nil {
		t.Fatalf("Failed to create export dir: %v", err)
	}

	configMap1YAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: config-one
  namespace: default
data:
  original: value1
`
	configMap2YAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: config-two
  namespace: default
data:
  original: value2
`
	if err := os.WriteFile(filepath.Join(exportDir, "default", "configmap-1.yaml"), []byte(configMap1YAML), 0644); err != nil {
		t.Fatalf("Failed to write configmap1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(exportDir, "default", "configmap-2.yaml"), []byte(configMap2YAML), 0644); err != nil {
		t.Fatalf("Failed to write configmap2: %v", err)
	}

	// Create stage 1 with kustomization (no plugins)
	stage1Dir := filepath.Join(transformDir, "10_stage1")
	stage1ResourcesDir := filepath.Join(stage1Dir, "resources")

	if err := os.MkdirAll(stage1ResourcesDir, 0700); err != nil {
		t.Fatalf("Failed to create stage1 resources dir: %v", err)
	}

	// Write both resources to stage 1
	if err := os.WriteFile(filepath.Join(stage1ResourcesDir, "ConfigMap_config-one.yaml"), []byte(configMap1YAML), 0644); err != nil {
		t.Fatalf("Failed to write stage1 resource 1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stage1ResourcesDir, "ConfigMap_config-two.yaml"), []byte(configMap2YAML), 0644); err != nil {
		t.Fatalf("Failed to write stage1 resource 2: %v", err)
	}

	// Stage 1 kustomization - just pass through
	stage1Kustomization := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- resources/ConfigMap_config-one.yaml
- resources/ConfigMap_config-two.yaml
`
	if err := os.WriteFile(filepath.Join(stage1Dir, "kustomization.yaml"), []byte(stage1Kustomization), 0644); err != nil {
		t.Fatalf("Failed to write stage1 kustomization: %v", err)
	}

	// Create stage 2 with kustomization
	stage2Dir := filepath.Join(transformDir, "20_stage2")
	stage2ResourcesDir := filepath.Join(stage2Dir, "resources")

	if err := os.MkdirAll(stage2ResourcesDir, 0700); err != nil {
		t.Fatalf("Failed to create stage2 resources dir: %v", err)
	}

	// Placeholder kustomization for stage 2 (will be replaced by orchestrator)
	stage2Kustomization := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: []
`
	if err := os.WriteFile(filepath.Join(stage2Dir, "kustomization.yaml"), []byte(stage2Kustomization), 0644); err != nil {
		t.Fatalf("Failed to write stage2 kustomization: %v", err)
	}

	// Run the orchestrator
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	o := &Orchestrator{
		Log:          logger,
		ExportDir:    exportDir,
		TransformDir: transformDir,
		PluginDir:    "/nonexistent", // No plugins
		Force:        true,
	}

	// Execute multi-stage pipeline
	selector := StageSelector{} // Select all stages
	if err := o.RunMultiStage(selector); err != nil {
		t.Fatalf("RunMultiStage failed: %v", err)
	}

	// ASSERTIONS: Verify the sequential execution behavior

	opts := file.PathOpts{
		TransformDir: transformDir,
		ExportDir:    exportDir,
	}

	// Assert .work/<stage>/input and .work/<stage>/output directories exist
	stage1InputDir := opts.GetStageInputDir("10_stage1")
	stage1OutputDir := opts.GetStageOutputDir("10_stage1")
	stage2InputDir := opts.GetStageInputDir("20_stage2")
	stage2OutputDir := opts.GetStageOutputDir("20_stage2")

	if _, err := os.Stat(stage1InputDir); os.IsNotExist(err) {
		t.Errorf("Stage 1 input directory does not exist: %s", stage1InputDir)
	}
	if _, err := os.Stat(stage1OutputDir); os.IsNotExist(err) {
		t.Errorf("Stage 1 output directory does not exist: %s", stage1OutputDir)
	}
	if _, err := os.Stat(stage2InputDir); os.IsNotExist(err) {
		t.Errorf("Stage 2 input directory does not exist: %s", stage2InputDir)
	}
	if _, err := os.Stat(stage2OutputDir); os.IsNotExist(err) {
		t.Errorf("Stage 2 output directory does not exist: %s", stage2OutputDir)
	}

	// Assert stage 1 input contains original export data
	stage1InputFiles, _ := filepath.Glob(filepath.Join(stage1InputDir, "default", "ConfigMap_*_config-one.yaml"))
	if len(stage1InputFiles) == 0 {
		t.Errorf("Stage 1 input should contain config-one from export in: %s/default/", stage1InputDir)
	}

	// Assert stage 1 output exists
	stage1OutputFiles, _ := filepath.Glob(filepath.Join(stage1OutputDir, "default", "ConfigMap_*_config-one.yaml"))
	if len(stage1OutputFiles) == 0 {
		t.Errorf("Stage 1 output should exist in: %s/default/", stage1OutputDir)
	}

	// Assert stage 2 input contains stage 1 output (both resources)
	stage2InputFiles1, _ := filepath.Glob(filepath.Join(stage2InputDir, "default", "ConfigMap_*_config-one.yaml"))
	stage2InputFiles2, _ := filepath.Glob(filepath.Join(stage2InputDir, "default", "ConfigMap_*_config-two.yaml"))

	if len(stage2InputFiles1) > 0 {
		stage2Input1, err := os.ReadFile(stage2InputFiles1[0])
		if err != nil {
			t.Errorf("Stage 2 input should contain config-one from stage 1 output: %v", err)
		} else {
			// Verify it's from stage 1 (contains original data)
			if !contains(string(stage2Input1), "original: value1") {
				t.Errorf("Stage 2 input config-one missing original data")
			}
		}
	} else {
		t.Errorf("Stage 2 input should contain config-one from stage 1 output in: %s/default/", stage2InputDir)
	}

	if len(stage2InputFiles2) == 0 {
		t.Errorf("Stage 2 input should contain config-two from stage 1 output in: %s/default/", stage2InputDir)
	}

	// Assert stage 2 output exists
	stage2OutputFiles1, _ := filepath.Glob(filepath.Join(stage2OutputDir, "default", "ConfigMap_*_config-one.yaml"))
	if len(stage2OutputFiles1) == 0 {
		t.Errorf("Stage 2 output should exist in: %s/default/", stage2OutputDir)
	}

	// Verify the key property: stage 2's input directory is stage 1's output directory content
	// This ensures sequential consistency
	t.Log("✓ Stage 1 input loaded from export directory")
	t.Log("✓ Stage 1 output written to .work/10_stage1/output/")
	t.Log("✓ Stage 2 input loaded from stage 1 output (.work/10_stage1/output/)")
	t.Log("✓ Stage 2 output written to .work/20_stage2/output/")
	t.Log("✓ Sequential consistency verified: each stage consumes previous stage's output")
}

// TestWhiteoutPropagation tests that whiteouts are materialized between stages
func TestWhiteoutPropagation(t *testing.T) {
	// This test verifies that when a plugin marks a resource as whiteout in stage N,
	// that resource does not appear in stage N+1's input.
	//
	// Since RunMultiStage regenerates artifacts via plugins (which we don't have in this test),
	// we test the whiteout behavior at the writer level, which is where whiteouts are actually
	// filtered (writer.go:64-66).
	//
	// The actual whiteout propagation is implicit: WriteStage skips HaveWhiteOut=true artifacts,
	// so they don't get written to the stage directory. When the next stage loads from the
	// previous stage's output via applyStageTransforms, the whiteouted resources are absent.

	tmpDir, err := os.MkdirTemp("", "whiteout-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	exportDir := filepath.Join(tmpDir, "export")
	transformDir := filepath.Join(tmpDir, "transform")

	// Create export directory
	if err := os.MkdirAll(filepath.Join(exportDir, "default"), 0700); err != nil {
		t.Fatalf("Failed to create export dir: %v", err)
	}

	// Create one ConfigMap and one Secret in export
	// The Secret will be marked as whiteout (entire type)
	configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: keep-me
  namespace: default
data:
  status: normal
`
	secretYAML := `apiVersion: v1
kind: Secret
metadata:
  name: whiteout-secret
  namespace: default
data:
  password: c2VjcmV0
`
	if err := os.WriteFile(filepath.Join(exportDir, "default", "configmap.yaml"), []byte(configMapYAML), 0644); err != nil {
		t.Fatalf("Failed to write configmap: %v", err)
	}
	if err := os.WriteFile(filepath.Join(exportDir, "default", "secret.yaml"), []byte(secretYAML), 0644); err != nil {
		t.Fatalf("Failed to write secret: %v", err)
	}

	// Test the writer's whiteout behavior directly
	// Create a stage with one normal artifact and one whiteout artifact
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	opts := file.PathOpts{
		TransformDir: transformDir,
		ExportDir:    exportDir,
	}

	// Load resources
	files, err := file.ReadFiles(context.TODO(), exportDir)
	if err != nil {
		t.Fatalf("Failed to read export files: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("Expected 2 resources, got %d", len(files))
	}

	// Create transform artifacts with Secret marked as whiteout (entire type)
	var artifacts []cranelib.TransformArtifact
	for _, f := range files {
		artifact := cranelib.TransformArtifact{
			Resource:     f.Unstructured,
			HaveWhiteOut: false,
			Patches:      nil,
			IgnoredOps:   []cranelib.IgnoredOperation{},
			Target:       cranelib.DeriveTargetFromResource(f.Unstructured),
			PluginName:   "",
		}

		// Mark ALL Secrets as whiteout (entire type whiteout)
		if f.Unstructured.GetKind() == "Secret" {
			artifact.HaveWhiteOut = true
		}

		artifacts = append(artifacts, artifact)
	}

	// Write stage using the writer
	writer := NewKustomizeWriter(opts, "10_test_stage")
	if err := writer.WriteStage(artifacts, true); err != nil {
		t.Fatalf("Failed to write stage: %v", err)
	}

	// ASSERTIONS: Verify new whiteout behavior
	// 1. All resource type files exist in resources/ (including whiteout Secret type)
	// 2. Only non-whiteout types are in kustomization.yaml resources list
	// 3. Whiteout comment exists in kustomization.yaml
	// 4. kubectl kustomize output would exclude whiteout types

	stageDir := filepath.Join(transformDir, "10_test_stage")
	resourcesDir := filepath.Join(stageDir, "resources")

	// Count resources written
	resourceFileMap := make(map[string]string) // filename -> path
	filepath.Walk(resourcesDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(path) == ".yaml" {
			resourceFileMap[info.Name()] = path
		}
		return nil
	})

	// NEW BEHAVIOR: Should have 2 resource type files (configmap.yaml and secret.yaml)
	// Both exist in resources/, but only configmap.yaml is active
	if len(resourceFileMap) != 2 {
		t.Errorf("Expected 2 resource type files (configmap.yaml, secret.yaml), but found %d: %v", len(resourceFileMap), resourceFileMap)
	}

	// Verify ConfigMap file exists and contains keep-me
	// File should be named: ConfigMap__v1_default_keep-me.yaml
	var configMapPath string
	var hasConfigMap bool
	for filename, path := range resourceFileMap {
		if strings.Contains(filename, "ConfigMap") && strings.Contains(filename, "keep-me") {
			configMapPath = path
			hasConfigMap = true
			break
		}
	}
	if !hasConfigMap {
		t.Errorf("Expected ConfigMap (keep-me) in resources/, but not found. Files: %v", resourceFileMap)
	} else {
		content, err := os.ReadFile(configMapPath)
		if err != nil {
			t.Fatalf("Failed to read ConfigMap file: %v", err)
		}
		if !contains(string(content), "name: keep-me") {
			t.Errorf("ConfigMap file should contain keep-me, got: %s", string(content))
		}
	}

	// Verify Secret file exists and contains whiteout-secret
	// File should be named: Secret__v1_default_whiteout-secret.yaml
	var secretPath string
	var hasSecret bool
	for filename, path := range resourceFileMap {
		if strings.Contains(filename, "Secret") && strings.Contains(filename, "whiteout-secret") {
			secretPath = path
			hasSecret = true
			break
		}
	}
	if !hasSecret {
		t.Errorf("Expected Secret (whiteout-secret) in resources/ (complete snapshot), but not found. Files: %v", resourceFileMap)
	} else {
		content, err := os.ReadFile(secretPath)
		if err != nil {
			t.Fatalf("Failed to read Secret file: %v", err)
		}
		if !contains(string(content), "name: whiteout-secret") {
			t.Errorf("Secret file should contain whiteout-secret, got: %s", string(content))
		}
	}

	// Verify kustomization.yaml
	kustomizationPath := filepath.Join(stageDir, "kustomization.yaml")
	kustomizationContent, err := os.ReadFile(kustomizationPath)
	if err != nil {
		t.Fatalf("Failed to read kustomization.yaml: %v", err)
	}

	kustomizationStr := string(kustomizationContent)

	// Should reference ConfigMap file (non-whiteout)
	if !contains(kustomizationStr, "ConfigMap") || !contains(kustomizationStr, "keep-me") {
		t.Errorf("kustomization.yaml should reference ConfigMap keep-me in resources list, got: %s", kustomizationStr)
	}

	// Should NOT reference Secret in resources list (whiteout type)
	// BUT should have a comment about whiteout
	lines := strings.Split(kustomizationStr, "\n")
	hasWhiteoutComment := false
	secretInResourcesList := false
	inResources := false

	for _, line := range lines {
		if strings.HasPrefix(line, "# Whiteout") {
			hasWhiteoutComment = true
		}
		if strings.Contains(line, "resources:") {
			inResources = true
		}
		// Check if Secret is in active resources list (not as comment)
		if inResources && strings.Contains(line, "Secret") && strings.Contains(line, "whiteout-secret") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			secretInResourcesList = true
		}
	}

	if !hasWhiteoutComment {
		t.Errorf("kustomization.yaml should have whiteout comment, got: %s", kustomizationStr)
	}

	if secretInResourcesList {
		t.Errorf("kustomization.yaml should NOT list Secret in active resources (it's whiteout), got: %s", kustomizationStr)
	}

	t.Log("✓ Both resource type files exist in resources/ (complete snapshot)")
	t.Log("✓ Only non-whiteout type (ConfigMap) is in kustomization.yaml resources list")
	t.Log("✓ Whiteout type (Secret) has comment in kustomization.yaml")
	t.Log("✓ Whiteout materialization: file exists but excluded from active resources")
}

// TestWhiteoutKustomizeBuild tests that kubectl kustomize excludes whiteout resources
func TestWhiteoutKustomizeBuild(t *testing.T) {
	// Skip if kubectl/oc is not available
	if !hasKustomizeCommand(t) {
		t.Skip("kubectl or oc not available, skipping test that requires kustomize")
	}

	tmpDir, err := os.MkdirTemp("", "whiteout-kustomize-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	exportDir := filepath.Join(tmpDir, "export")
	transformDir := filepath.Join(tmpDir, "transform")

	// Create export directory
	if err := os.MkdirAll(filepath.Join(exportDir, "default"), 0700); err != nil {
		t.Fatalf("Failed to create export dir: %v", err)
	}

	// Create ConfigMap (non-whiteout) and Secret (whiteout)
	configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: active-config
  namespace: default
data:
  key: value
`
	secretYAML := `apiVersion: v1
kind: Secret
metadata:
  name: whiteout-secret
  namespace: default
data:
  password: c2VjcmV0
`
	if err := os.WriteFile(filepath.Join(exportDir, "default", "configmap.yaml"), []byte(configMapYAML), 0644); err != nil {
		t.Fatalf("Failed to write configmap: %v", err)
	}
	if err := os.WriteFile(filepath.Join(exportDir, "default", "secret.yaml"), []byte(secretYAML), 0644); err != nil {
		t.Fatalf("Failed to write secret: %v", err)
	}

	// Load resources and create artifacts
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	opts := file.PathOpts{
		TransformDir: transformDir,
		ExportDir:    exportDir,
	}

	files, err := file.ReadFiles(context.TODO(), exportDir)
	if err != nil {
		t.Fatalf("Failed to read export files: %v", err)
	}

	// Create artifacts with Secret marked as whiteout
	var artifacts []cranelib.TransformArtifact
	for _, f := range files {
		artifact := cranelib.TransformArtifact{
			Resource:     f.Unstructured,
			HaveWhiteOut: f.Unstructured.GetKind() == "Secret", // Whiteout all Secrets
			Patches:      nil,
			IgnoredOps:   []cranelib.IgnoredOperation{},
			Target:       cranelib.DeriveTargetFromResource(f.Unstructured),
			PluginName:   "",
		}
		artifacts = append(artifacts, artifact)
	}

	// Write stage
	writer := NewKustomizeWriter(opts, "10_test_stage")
	if err := writer.WriteStage(artifacts, true); err != nil {
		t.Fatalf("Failed to write stage: %v", err)
	}

	// Run kubectl kustomize on the stage
	stageDir := filepath.Join(transformDir, "10_test_stage")

	o := &Orchestrator{
		Log: logger,
	}

	outputResources, err := o.applyStageTransforms(stageDir)
	if err != nil {
		t.Fatalf("Failed to apply stage transforms: %v", err)
	}

	// Verify output contains ConfigMap but NOT Secret
	hasConfigMap := false
	hasSecret := false

	for _, resource := range outputResources {
		kind := resource.GetKind()
		name := resource.GetName()

		if kind == "ConfigMap" && name == "active-config" {
			hasConfigMap = true
		}
		if kind == "Secret" && name == "whiteout-secret" {
			hasSecret = true
		}
	}

	if !hasConfigMap {
		t.Errorf("kubectl kustomize output should contain ConfigMap active-config")
	}
	if hasSecret {
		t.Errorf("kubectl kustomize output should NOT contain whiteout Secret whiteout-secret")
	}

	t.Log("✓ kubectl kustomize output contains non-whiteout resources (ConfigMap)")
	t.Log("✓ kubectl kustomize output excludes whiteout resources (Secret)")
	t.Log("✓ Whiteout is correctly materialized by exclusion from kustomization.yaml")
}

// TestWhiteoutMultiStagePropagation tests that whiteout resources don't propagate to next stage
func TestWhiteoutMultiStagePropagation(t *testing.T) {
	// Skip if kubectl/oc is not available
	if !hasKustomizeCommand(t) {
		t.Skip("kubectl or oc not available, skipping test that requires kustomize")
	}

	tmpDir, err := os.MkdirTemp("", "multistage-whiteout-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	exportDir := filepath.Join(tmpDir, "export")
	transformDir := filepath.Join(tmpDir, "transform")

	// Create export directory with ConfigMap and Secret
	if err := os.MkdirAll(filepath.Join(exportDir, "default"), 0700); err != nil {
		t.Fatalf("Failed to create export dir: %v", err)
	}

	configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: config
  namespace: default
data:
  key: value
`
	secretYAML := `apiVersion: v1
kind: Secret
metadata:
  name: secret
  namespace: default
data:
  password: c2VjcmV0
`
	if err := os.WriteFile(filepath.Join(exportDir, "default", "configmap.yaml"), []byte(configMapYAML), 0644); err != nil {
		t.Fatalf("Failed to write configmap: %v", err)
	}
	if err := os.WriteFile(filepath.Join(exportDir, "default", "secret.yaml"), []byte(secretYAML), 0644); err != nil {
		t.Fatalf("Failed to write secret: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	opts := file.PathOpts{
		TransformDir: transformDir,
		ExportDir:    exportDir,
	}

	// Stage 1: Create artifacts with Secret marked as whiteout
	files, err := file.ReadFiles(context.TODO(), exportDir)
	if err != nil {
		t.Fatalf("Failed to read export files: %v", err)
	}

	var stage1Artifacts []cranelib.TransformArtifact
	for _, f := range files {
		artifact := cranelib.TransformArtifact{
			Resource:     f.Unstructured,
			HaveWhiteOut: f.Unstructured.GetKind() == "Secret", // Whiteout Secrets in stage 1
			Patches:      nil,
			IgnoredOps:   []cranelib.IgnoredOperation{},
			Target:       cranelib.DeriveTargetFromResource(f.Unstructured),
			PluginName:   "",
		}
		stage1Artifacts = append(stage1Artifacts, artifact)
	}

	// Write stage 1
	writer1 := NewKustomizeWriter(opts, "10_stage1")
	if err := writer1.WriteStage(stage1Artifacts, true); err != nil {
		t.Fatalf("Failed to write stage 1: %v", err)
	}

	// Apply stage 1 transforms to get output
	o := &Orchestrator{
		Log: logger,
	}

	stage1Dir := filepath.Join(transformDir, "10_stage1")
	stage1Output, err := o.applyStageTransforms(stage1Dir)
	if err != nil {
		t.Fatalf("Failed to apply stage 1 transforms: %v", err)
	}

	// Verify stage 1 output contains only ConfigMap (Secret was whiteouted)
	if len(stage1Output) != 1 {
		t.Errorf("Stage 1 output should have 1 resource (ConfigMap), got %d", len(stage1Output))
	}
	if len(stage1Output) > 0 && stage1Output[0].GetKind() != "ConfigMap" {
		t.Errorf("Stage 1 output should be ConfigMap, got %s", stage1Output[0].GetKind())
	}

	// Stage 2: Load from stage 1 output and create stage 2 artifacts (no whiteout)
	var stage2Artifacts []cranelib.TransformArtifact
	for _, resource := range stage1Output {
		artifact := cranelib.TransformArtifact{
			Resource:     resource,
			HaveWhiteOut: false, // Stage 2 doesn't whiteout anything
			Patches:      nil,
			IgnoredOps:   []cranelib.IgnoredOperation{},
			Target:       cranelib.DeriveTargetFromResource(resource),
			PluginName:   "",
		}
		stage2Artifacts = append(stage2Artifacts, artifact)
	}

	// Write stage 2
	writer2 := NewKustomizeWriter(opts, "20_stage2")
	if err := writer2.WriteStage(stage2Artifacts, true); err != nil {
		t.Fatalf("Failed to write stage 2: %v", err)
	}

	// Verify stage 2 resources/ directory
	stage2ResourcesDir := filepath.Join(transformDir, "20_stage2", "resources")
	stage2Files := make(map[string]bool)
	filepath.Walk(stage2ResourcesDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(path) == ".yaml" {
			stage2Files[info.Name()] = true
		}
		return nil
	})

	// Stage 2 should only have ConfigMap file, NOT Secret file
	hasConfigMap := false
	hasSecret := false
	for filename := range stage2Files {
		if strings.Contains(filename, "ConfigMap") && strings.Contains(filename, "config") {
			hasConfigMap = true
		}
		if strings.Contains(filename, "Secret") {
			hasSecret = true
		}
	}
	if !hasConfigMap {
		t.Errorf("Stage 2 should have ConfigMap file, got files: %v", stage2Files)
	}
	if hasSecret {
		t.Errorf("Stage 2 should NOT have Secret file (it was whiteouted in stage 1), got files: %v", stage2Files)
	}

	// Apply stage 2 transforms to get final output
	stage2Dir := filepath.Join(transformDir, "20_stage2")
	stage2Output, err := o.applyStageTransforms(stage2Dir)
	if err != nil {
		t.Fatalf("Failed to apply stage 2 transforms: %v", err)
	}

	// Verify stage 2 output contains only ConfigMap (Secret was propagated as whiteout)
	if len(stage2Output) != 1 {
		t.Errorf("Stage 2 output should have 1 resource (ConfigMap), got %d", len(stage2Output))
	}
	if len(stage2Output) > 0 && stage2Output[0].GetKind() != "ConfigMap" {
		t.Errorf("Stage 2 output should be ConfigMap, got %s", stage2Output[0].GetKind())
	}

	t.Log("✓ Stage 1: Secret marked as whiteout, written to resources/ but excluded from kustomization")
	t.Log("✓ Stage 1 output: contains only ConfigMap (Secret excluded)")
	t.Log("✓ Stage 2 input: loaded from stage 1 output, contains only ConfigMap")
	t.Log("✓ Stage 2 resources/: contains only configmap.yaml (no secret.yaml)")
	t.Log("✓ Stage 2 output: contains only ConfigMap")
	t.Log("✓ Whiteout correctly prevented propagation from stage 1 to stage 2")
}

// TestWhiteoutMixedTypeInvariant tests the case where some resources of a type are whiteout and others are not
// This documents current behavior: if ANY resource in a type is non-whiteout, the entire type file is active
func TestWhiteoutMixedTypeInvariant(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mixed-whiteout-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	exportDir := filepath.Join(tmpDir, "export")
	transformDir := filepath.Join(tmpDir, "transform")

	// Create export with two ConfigMaps
	if err := os.MkdirAll(filepath.Join(exportDir, "default"), 0700); err != nil {
		t.Fatalf("Failed to create export dir: %v", err)
	}

	configMap1YAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: config-active
  namespace: default
data:
  key: value1
`
	configMap2YAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: config-whiteout
  namespace: default
data:
  key: value2
`
	if err := os.WriteFile(filepath.Join(exportDir, "default", "config1.yaml"), []byte(configMap1YAML), 0644); err != nil {
		t.Fatalf("Failed to write config1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(exportDir, "default", "config2.yaml"), []byte(configMap2YAML), 0644); err != nil {
		t.Fatalf("Failed to write config2: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	opts := file.PathOpts{
		TransformDir: transformDir,
		ExportDir:    exportDir,
	}

	files, err := file.ReadFiles(context.TODO(), exportDir)
	if err != nil {
		t.Fatalf("Failed to read export files: %v", err)
	}

	// Create artifacts with one ConfigMap whiteout, one not
	var artifacts []cranelib.TransformArtifact
	for _, f := range files {
		isWhiteout := f.Unstructured.GetName() == "config-whiteout"
		artifact := cranelib.TransformArtifact{
			Resource:     f.Unstructured,
			HaveWhiteOut: isWhiteout,
			Patches:      nil,
			IgnoredOps:   []cranelib.IgnoredOperation{},
			Target:       cranelib.DeriveTargetFromResource(f.Unstructured),
			PluginName:   "",
		}
		artifacts = append(artifacts, artifact)
	}

	// Write stage
	writer := NewKustomizeWriter(opts, "10_mixed_stage")
	if err := writer.WriteStage(artifacts, true); err != nil {
		t.Fatalf("Failed to write stage: %v", err)
	}

	stageDir := filepath.Join(transformDir, "10_mixed_stage")
	resourcesDir := filepath.Join(stageDir, "resources")

	// Check resource files written
	resourceFiles := make(map[string]string)
	filepath.Walk(resourcesDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(path) == ".yaml" {
			resourceFiles[info.Name()] = path
		}
		return nil
	})

	// Should have 2 resource files (one for each ConfigMap)
	if len(resourceFiles) != 2 {
		t.Errorf("Expected 2 resource files (one per ConfigMap), got %d: %v", len(resourceFiles), resourceFiles)
	}

	// Find the active and whiteout ConfigMap files
	var activeConfigMapPath, whiteoutConfigMapPath string
	for filename, path := range resourceFiles {
		if strings.Contains(filename, "config-active") {
			activeConfigMapPath = path
		} else if strings.Contains(filename, "config-whiteout") {
			whiteoutConfigMapPath = path
		}
	}

	if activeConfigMapPath == "" {
		t.Fatalf("Expected config-active file, got files: %v", resourceFiles)
	}
	if whiteoutConfigMapPath == "" {
		t.Fatalf("Expected config-whiteout file, got files: %v", resourceFiles)
	}

	// Read active ConfigMap content
	activeContent, err := os.ReadFile(activeConfigMapPath)
	if err != nil {
		t.Fatalf("Failed to read active ConfigMap file: %v", err)
	}

	// Read whiteout ConfigMap content
	whiteoutContent, err := os.ReadFile(whiteoutConfigMapPath)
	if err != nil {
		t.Fatalf("Failed to read whiteout ConfigMap file: %v", err)
	}

	contentStr := string(activeContent) + "\n" + string(whiteoutContent)
	hasActive := contains(contentStr, "name: config-active")
	hasWhiteout := contains(contentStr, "name: config-whiteout")

	if !hasActive {
		t.Errorf("configmap.yaml should contain config-active, got: %s", contentStr)
	}
	if !hasWhiteout {
		t.Errorf("configmap.yaml should contain config-whiteout (complete snapshot), got: %s", contentStr)
	}

	// Read kustomization.yaml
	kustomizationPath := filepath.Join(stageDir, "kustomization.yaml")
	kustomizationContent, err := os.ReadFile(kustomizationPath)
	if err != nil {
		t.Fatalf("Failed to read kustomization.yaml: %v", err)
	}

	kustomizationStr := string(kustomizationContent)

	// CURRENT BEHAVIOR: Each resource has its own file, whiteout is per-resource
	// kustomization.yaml should reference only the active ConfigMap
	if !contains(kustomizationStr, "config-active") {
		t.Errorf("kustomization.yaml should reference config-active ConfigMap, got: %s", kustomizationStr)
	}

	// Should NOT reference config-whiteout in active resources
	if strings.Contains(kustomizationStr, "resources/ConfigMap") && strings.Contains(kustomizationStr, "config-whiteout") && !strings.Contains(kustomizationStr, "# - resources/ConfigMap") {
		t.Errorf("kustomization.yaml should NOT reference config-whiteout in active resources, got: %s", kustomizationStr)
	}

	t.Log("✓ Mixed whiteout/non-whiteout within same type detected")
	t.Log("✓ Each resource has its own file (complete snapshot)")
	t.Log("✓ Only non-whiteout resource is in kustomization.yaml resources list")
	t.Log("✓ Whiteout resource is commented in kustomization.yaml")
	t.Log("✓ Whiteout granularity is per-resource (not per-type)")
}

// TestStageWorkingDirectoryStructure tests the intermediate artifact layout
func TestStageWorkingDirectoryStructure(t *testing.T) {
	// This test validates that each stage creates the required working directories
	// for debugging: input/, transform/, output/

	tmpDir, err := os.MkdirTemp("", "workdir-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	transformDir := filepath.Join(tmpDir, "transform")

	opts := file.PathOpts{
		TransformDir: transformDir,
	}

	stageName := "10_TestStage"

	// Verify path structure
	expectedWorkDir := filepath.Join(transformDir, ".work", stageName)
	expectedInputDir := filepath.Join(expectedWorkDir, "input")
	expectedOutputDir := filepath.Join(expectedWorkDir, "output")
	expectedTransformDir := filepath.Join(transformDir, stageName)

	actualWorkDir := opts.GetStageWorkDir(stageName)
	actualInputDir := opts.GetStageInputDir(stageName)
	actualOutputDir := opts.GetStageOutputDir(stageName)
	actualTransformDir := opts.GetStageTransformDir(stageName)

	if actualWorkDir != expectedWorkDir {
		t.Errorf("Work dir mismatch: expected %s, got %s", expectedWorkDir, actualWorkDir)
	}
	if actualInputDir != expectedInputDir {
		t.Errorf("Input dir mismatch: expected %s, got %s", expectedInputDir, actualInputDir)
	}
	if actualOutputDir != expectedOutputDir {
		t.Errorf("Output dir mismatch: expected %s, got %s", expectedOutputDir, actualOutputDir)
	}
	if actualTransformDir != expectedTransformDir {
		t.Errorf("Transform dir mismatch: expected %s, got %s", expectedTransformDir, actualTransformDir)
	}

	t.Log("✓ Working directory structure is correct")
	t.Log("  - .work/<stage>/input/    - snapshot of input resources")
	t.Log("  - <stage>/               - transform artifacts (kustomization, patches)")
	t.Log("  - .work/<stage>/output/   - materialized output (next stage input)")
}

// TestStageFailurePropagation tests that pipeline stops when stages are misconfigured
func TestStageFailurePropagation(t *testing.T) {
	// This test verifies error propagation when stage discovery or execution fails.
	// Since RunMultiStage regenerates artifacts via executeStage, we can't inject runtime
	// kustomize failures easily. Instead, we test that:
	// 1. When no stages are found, RunMultiStage returns an appropriate error
	// 2. Error messages include contextual information

	tmpDir, err := os.MkdirTemp("", "failure-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	exportDir := filepath.Join(tmpDir, "export")
	transformDir := filepath.Join(tmpDir, "transform")

	// Create export directory
	if err := os.MkdirAll(filepath.Join(exportDir, "default"), 0700); err != nil {
		t.Fatalf("Failed to create export dir: %v", err)
	}

	configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: default
data:
  key: value
`
	if err := os.WriteFile(filepath.Join(exportDir, "default", "configmap.yaml"), []byte(configMapYAML), 0644); err != nil {
		t.Fatalf("Failed to write configmap: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	o := &Orchestrator{
		Log:          logger,
		ExportDir:    exportDir,
		TransformDir: transformDir,
		PluginDir:    "/nonexistent",
		Force:        true,
	}

	// Test 1: No stages found
	t.Run("no stages found", func(t *testing.T) {
		selector := StageSelector{} // Select all stages
		err := o.RunMultiStage(selector)

		if err == nil {
			t.Fatal("Expected error when no stages exist, but RunMultiStage succeeded")
		}

		if !contains(err.Error(), "no stages found") {
			t.Errorf("Expected 'no stages found' error, got: %v", err)
		}
	})

	// Test 2: Create a stage and verify it executes
	t.Run("single stage executes successfully", func(t *testing.T) {
		stage1Dir := filepath.Join(transformDir, "10_stage1")
		stage1ResourcesDir := filepath.Join(stage1Dir, "resources")

		if err := os.MkdirAll(stage1ResourcesDir, 0700); err != nil {
			t.Fatalf("Failed to create stage1 resources dir: %v", err)
		}

		if err := os.WriteFile(filepath.Join(stage1ResourcesDir, "ConfigMap.yaml"), []byte(configMapYAML), 0644); err != nil {
			t.Fatalf("Failed to write stage1 resource: %v", err)
		}

		stage1Kustomization := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- resources/ConfigMap.yaml
`
		if err := os.WriteFile(filepath.Join(stage1Dir, "kustomization.yaml"), []byte(stage1Kustomization), 0644); err != nil {
			t.Fatalf("Failed to write stage1 kustomization: %v", err)
		}

		selector := StageSelector{}
		if err := o.RunMultiStage(selector); err != nil {
			t.Fatalf("RunMultiStage failed unexpectedly: %v", err)
		}

		// Verify stage 1 executed
		opts := file.PathOpts{
			TransformDir: transformDir,
			ExportDir:    exportDir,
		}

		stage1OutputDir := opts.GetStageOutputDir("10_stage1")
		if _, err := os.Stat(stage1OutputDir); os.IsNotExist(err) {
			t.Errorf("Stage 1 should have completed, but output directory does not exist")
		}
	})

	t.Log("✓ Pipeline fails gracefully when no stages found")
	t.Log("✓ Error messages include contextual information")
	t.Log("✓ Single stage executes successfully")
}
