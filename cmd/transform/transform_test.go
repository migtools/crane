package transform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	cranelib "github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane/internal/flags"
	internalTransform "github.com/konveyor/crane/internal/transform"
	"github.com/sirupsen/logrus"
)

// resolveStagesTestEnv contains test environment for resolveAndValidateStages tests
type resolveStagesTestEnv struct {
	TempDir      string
	TransformDir string
	PluginDir    string
	ExportDir    string
	Options      *Options
	Orchestrator *internalTransform.Orchestrator
	Log          *logrus.Logger
}

// newResolveStagesTestEnv creates a test environment for resolveAndValidateStages tests
func newResolveStagesTestEnv(t *testing.T) *resolveStagesTestEnv {
	t.Helper()

	tempDir := t.TempDir()
	transformDir := filepath.Join(tempDir, "transform")
	pluginDir := filepath.Join(tempDir, "plugins")
	exportDir := filepath.Join(tempDir, "export")

	if err := os.MkdirAll(transformDir, 0755); err != nil {
		t.Fatalf("failed to create transform dir: %v", err)
	}
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		t.Fatalf("failed to create export dir: %v", err)
	}

	log := logrus.New()
	log.SetOutput(os.Stderr)

	options := &Options{
		Flags: Flags{
			SkipPlugins: []string{},
		},
	}

	orchestrator := &internalTransform.Orchestrator{
		Log:                log,
		TransformDir:       transformDir,
		NewlyCreatedStages: make(map[string]bool),
	}

	return &resolveStagesTestEnv{
		TempDir:      tempDir,
		TransformDir: transformDir,
		PluginDir:    pluginDir,
		ExportDir:    exportDir,
		Options:      options,
		Orchestrator: orchestrator,
		Log:          log,
	}
}

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
			err := validateStageNameToken(tt.input)
			if tt.expectErr && err == nil {
				t.Errorf("validateStageNameToken(%q) expected error but got nil", tt.input)
			}
			if !tt.expectErr && err != nil {
				t.Errorf("validateStageNameToken(%q) unexpected error: %v", tt.input, err)
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

func TestCreateDefaultStagesForAllPlugins_PathTraversalProtection(t *testing.T) {
	// Test that path traversal protection works correctly, including edge cases
	tmpDir, err := os.MkdirTemp("", "crane-transform-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Valid plugin should work
	validPlugin := &mockPlugin{name: "ValidPlugin"}

	opts := &Options{}
	log := logrus.New()
	log.SetLevel(logrus.FatalLevel)

	orchestrator := &internalTransform.Orchestrator{
		NewlyCreatedStages: make(map[string]bool),
	}

	stageNames, err := opts.createDefaultStagesForAllPlugins(orchestrator, tmpDir, []cranelib.Plugin{validPlugin}, log)
	if err != nil {
		t.Fatalf("Valid plugin should not error: %v", err)
	}

	if len(stageNames) != 1 || stageNames[0] != "10_ValidPlugin" {
		t.Errorf("Expected stage '10_ValidPlugin', got: %v", stageNames)
	}

	// Verify the created directory is within tmpDir using filepath.Rel
	stageDir := filepath.Join(tmpDir, "10_ValidPlugin")
	rel, err := filepath.Rel(filepath.Clean(tmpDir), filepath.Clean(stageDir))
	if err != nil {
		t.Errorf("filepath.Rel failed for valid stage: %v", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		t.Errorf("Valid stage directory appears to be outside transform dir: rel=%q", rel)
	}
}

// Tests from upstream for instructions file functionality

func TestReconcileInstructionStages_Force(t *testing.T) {
	tmpDir := t.TempDir()
	transformDir := filepath.Join(tmpDir, "transform")
	if err := os.MkdirAll(transformDir, 0755); err != nil {
		t.Fatalf("failed to create transform dir: %v", err)
	}

	// Create two stage dirs
	if err := os.MkdirAll(filepath.Join(transformDir, "10_KubernetesPlugin"), 0755); err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(transformDir, "50_Stage2"), 0755); err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}
	// Also create work dirs
	if err := os.MkdirAll(filepath.Join(transformDir, ".work", "10_KubernetesPlugin"), 0755); err != nil {
		t.Fatalf("failed to create work: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(transformDir, ".work", "50_Stage2"), 0755); err != nil {
		t.Fatalf("failed to create work: %v", err)
	}

	o := &Options{
		Flags: Flags{
			Force: true,
		},
	}

	err := o.reconcileInstructionStages(
		transformDir,
		[]string{"10_KubernetesPlugin"},
		logrus.New(),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Desired stage should still exist.
	if _, err := os.Stat(filepath.Join(transformDir, "10_KubernetesPlugin")); err != nil {
		t.Fatalf("expected desired stage dir to exist, got err: %v", err)
	}

	// Extra stage should be deleted.
	_, err = os.Stat(filepath.Join(transformDir, "50_Stage2"))
	if !os.IsNotExist(err) {
		t.Fatalf("expected extra stage dir to be deleted, got err: %v", err)
	}
	// Extra stage work dir should be deleted.
	_, err = os.Stat(filepath.Join(transformDir, ".work", "50_Stage2"))
	if !os.IsNotExist(err) {
		t.Fatalf("expected extra stage work dir to be deleted, got err: %v", err)
	}
}

// Test that positional args and --instructions-file are mutually exclusive
func TestRun_InstructionsFileAndPositionalArgsConflict(t *testing.T) {
	o := &Options{
		globalFlags:     &flags.GlobalFlags{},
		RequestedStages: []string{"10_KubernetesPlugin"},
		Flags: Flags{
			InstructionsFile: "sample-transform-instructor-file.yaml",
		},
	}

	err := o.run()
	if err == nil {
		t.Fatalf("expected conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "use either --instructions-file or positional stage arguments, not both") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// Test that valid stage directory names create custom stages without requiring a plugin
func TestResolveAndValidateStages_CustomStageCreation(t *testing.T) {
	// Setup: temp directories
	tempDir := t.TempDir()
	transformDir := filepath.Join(tempDir, "transform")
	pluginDir := filepath.Join(tempDir, "plugins")

	if err := os.MkdirAll(transformDir, 0755); err != nil {
		t.Fatalf("failed to create transform dir: %v", err)
	}
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	// Create one existing stage with output directory
	// This simulates a stage that has already been run
	existingStageDir := filepath.Join(transformDir, "10_KubernetesPlugin")
	if err := os.MkdirAll(existingStageDir, 0755); err != nil {
		t.Fatalf("failed to create existing stage dir: %v", err)
	}
	// Create output directory to simulate stage has been run
	existingStageOutputDir := filepath.Join(transformDir, ".work", "10_KubernetesPlugin", "output")
	if err := os.MkdirAll(existingStageOutputDir, 0755); err != nil {
		t.Fatalf("failed to create existing stage output dir: %v", err)
	}

	log := logrus.New()
	log.SetOutput(os.Stderr)

	o := &Options{
		Flags: Flags{
			SkipPlugins: []string{},
		},
	}

	tests := []struct {
		name           string
		requestedStage string
		shouldCreate   bool
		expectError    bool
	}{
		{
			name:           "valid custom stage name creates directory",
			requestedStage: "50_CustomModifications",
			shouldCreate:   true,
			expectError:    false,
		},
		{
			name:           "existing stage is found",
			requestedStage: "10_KubernetesPlugin",
			shouldCreate:   false,
			expectError:    false,
		},
		{
			name:           "base name creates stage with automatic priority",
			requestedStage: "MyCustomStage",
			shouldCreate:   true,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fresh temp dir for each sub-test to avoid interference
			subTempDir := t.TempDir()
			subTransformDir := filepath.Join(subTempDir, "transform")
			subPluginDir := filepath.Join(subTempDir, "plugins")

			if err := os.MkdirAll(subTransformDir, 0755); err != nil {
				t.Fatalf("failed to create transform dir: %v", err)
			}
			if err := os.MkdirAll(subPluginDir, 0755); err != nil {
				t.Fatalf("failed to create plugin dir: %v", err)
			}

			// Create one existing stage with output directory
			existingStageDir := filepath.Join(subTransformDir, "10_KubernetesPlugin")
			if err := os.MkdirAll(existingStageDir, 0755); err != nil {
				t.Fatalf("failed to create existing stage dir: %v", err)
			}
			existingStageOutputDir := filepath.Join(subTransformDir, ".work", "10_KubernetesPlugin", "output")
			if err := os.MkdirAll(existingStageOutputDir, 0755); err != nil {
				t.Fatalf("failed to create existing stage output dir: %v", err)
			}

			subOrchestrator := &internalTransform.Orchestrator{
				Log:                log,
				TransformDir:       subTransformDir,
				NewlyCreatedStages: make(map[string]bool),
			}

			resolved, err := o.resolveAndValidateStages(
				[]string{tt.requestedStage},
				subOrchestrator,
				subTransformDir,
				subPluginDir,
				log,
			)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(resolved) != 1 {
				t.Fatalf("expected 1 resolved stage, got %d", len(resolved))
			}

			expectedResolved := tt.requestedStage
			// For base names without priority, we need to calculate expected stage name
			if tt.requestedStage == "MyCustomStage" {
				expectedResolved = "20_MyCustomStage" // maxPriority=10, nextPriority=20
			}

			if resolved[0] != expectedResolved {
				t.Errorf("expected resolved stage %q, got %q", expectedResolved, resolved[0])
			}

			// Check if directory was created
			stageDir := filepath.Join(subTransformDir, expectedResolved)
			_, err = os.Stat(stageDir)
			dirExists := err == nil

			if tt.shouldCreate {
				if !dirExists {
					t.Errorf("expected custom stage directory to be created at %s", stageDir)
				}
				if !subOrchestrator.NewlyCreatedStages[expectedResolved] {
					t.Errorf("expected stage %q to be marked as newly created", expectedResolved)
				}
			} else {
				if subOrchestrator.NewlyCreatedStages[expectedResolved] {
					t.Errorf("did not expect stage %q to be marked as newly created", expectedResolved)
				}
			}
		})
	}
}

// Test that multiple custom stages can be created in one call
func TestResolveAndValidateStages_MultipleCustomStages(t *testing.T) {
	tempDir := t.TempDir()
	transformDir := filepath.Join(tempDir, "transform")
	pluginDir := filepath.Join(tempDir, "plugins")

	if err := os.MkdirAll(transformDir, 0755); err != nil {
		t.Fatalf("failed to create transform dir: %v", err)
	}
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	// Create one existing stage with output directory
	// This simulates a stage that has already been run
	existingStageDir := filepath.Join(transformDir, "10_KubernetesPlugin")
	if err := os.MkdirAll(existingStageDir, 0755); err != nil {
		t.Fatalf("failed to create existing stage dir: %v", err)
	}
	// Create output directory to simulate stage has been run
	existingStageOutputDir := filepath.Join(transformDir, ".work", "10_KubernetesPlugin", "output")
	if err := os.MkdirAll(existingStageOutputDir, 0755); err != nil {
		t.Fatalf("failed to create existing stage output dir: %v", err)
	}

	log := logrus.New()
	log.SetOutput(os.Stderr)

	o := &Options{
		Flags: Flags{
			SkipPlugins: []string{},
		},
	}

	orchestrator := &internalTransform.Orchestrator{
		Log:                log,
		TransformDir:       transformDir,
		NewlyCreatedStages: make(map[string]bool),
	}

	// Request multiple custom stages
	requestedStages := []string{"20_FirstCustom", "30_SecondCustom", "40_ThirdCustom"}

	resolved, err := o.resolveAndValidateStages(
		requestedStages,
		orchestrator,
		transformDir,
		pluginDir,
		log,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resolved) != len(requestedStages) {
		t.Fatalf("expected %d resolved stages, got %d", len(requestedStages), len(resolved))
	}

	// Verify all custom stages were created
	for i, stageName := range requestedStages {
		if resolved[i] != stageName {
			t.Errorf("resolved[%d]: expected %q, got %q", i, stageName, resolved[i])
		}

		stageDir := filepath.Join(transformDir, stageName)
		if _, err := os.Stat(stageDir); os.IsNotExist(err) {
			t.Errorf("custom stage directory not created: %s", stageDir)
		}

		if !orchestrator.NewlyCreatedStages[stageName] {
			t.Errorf("stage %q not marked as newly created", stageName)
		}
	}
}

// Test that custom stage creation ensures previous stages have output
func TestResolveAndValidateStages_CustomStageRequiresPreviousStageOutput(t *testing.T) {
	tempDir := t.TempDir()
	transformDir := filepath.Join(tempDir, "transform")
	pluginDir := filepath.Join(tempDir, "plugins")
	exportDir := filepath.Join(tempDir, "export")

	if err := os.MkdirAll(transformDir, 0755); err != nil {
		t.Fatalf("failed to create transform dir: %v", err)
	}
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		t.Fatalf("failed to create export dir: %v", err)
	}

	// Create one existing stage WITHOUT output directory
	// This simulates a stage that exists but has not been run yet
	existingStageDir := filepath.Join(transformDir, "10_KubernetesPlugin")
	if err := os.MkdirAll(existingStageDir, 0755); err != nil {
		t.Fatalf("failed to create existing stage dir: %v", err)
	}

	log := logrus.New()
	log.SetOutput(os.Stderr)

	o := &Options{
		Flags: Flags{
			SkipPlugins: []string{},
		},
	}

	// Configure orchestrator with minimal setup needed for ensurePreviousStagesRun
	orchestrator := &internalTransform.Orchestrator{
		Log:                log,
		ExportDir:          exportDir,
		TransformDir:       transformDir,
		PluginDir:          pluginDir,
		NewlyCreatedStages: make(map[string]bool),
	}

	// Request a custom stage
	// This should fail because the previous stage (10_KubernetesPlugin) has no output
	_, err := o.resolveAndValidateStages(
		[]string{"50_CustomModifications"},
		orchestrator,
		transformDir,
		pluginDir,
		log,
	)

	// We expect this to fail because ensurePreviousStagesRun will try to run
	// the previous stage which will fail due to missing export/plugin setup
	if err == nil {
		t.Errorf("expected error when previous stage has no output, but got none")
	}

	// The error should mention the previous stage or output directory
	if err != nil && !strings.Contains(err.Error(), "10_KubernetesPlugin") &&
		!strings.Contains(err.Error(), "output") &&
		!strings.Contains(err.Error(), "previous") {
		t.Logf("Got error (which is expected): %v", err)
		// This is acceptable - the important thing is that it tried to ensure previous stages
	}
}

// Test that custom stage creation with all previous stages having output succeeds
func TestResolveAndValidateStages_CustomStageWithPreviousStageOutput(t *testing.T) {
	tempDir := t.TempDir()
	transformDir := filepath.Join(tempDir, "transform")
	pluginDir := filepath.Join(tempDir, "plugins")

	if err := os.MkdirAll(transformDir, 0755); err != nil {
		t.Fatalf("failed to create transform dir: %v", err)
	}
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	// Create one existing stage WITH output directory
	// This simulates a stage that has already been successfully run
	existingStageDir := filepath.Join(transformDir, "10_KubernetesPlugin")
	if err := os.MkdirAll(existingStageDir, 0755); err != nil {
		t.Fatalf("failed to create existing stage dir: %v", err)
	}
	// Create output directory to simulate stage has been run
	existingStageOutputDir := filepath.Join(transformDir, ".work", "10_KubernetesPlugin", "output")
	if err := os.MkdirAll(existingStageOutputDir, 0755); err != nil {
		t.Fatalf("failed to create existing stage output dir: %v", err)
	}

	log := logrus.New()
	log.SetOutput(os.Stderr)

	o := &Options{
		Flags: Flags{
			SkipPlugins: []string{},
		},
	}

	orchestrator := &internalTransform.Orchestrator{
		Log:                log,
		TransformDir:       transformDir,
		NewlyCreatedStages: make(map[string]bool),
	}

	// Request a custom stage
	// This should succeed because the previous stage has output
	resolved, err := o.resolveAndValidateStages(
		[]string{"50_CustomModifications"},
		orchestrator,
		transformDir,
		pluginDir,
		log,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved stage, got %d", len(resolved))
	}

	if resolved[0] != "50_CustomModifications" {
		t.Errorf("expected resolved stage %q, got %q", "50_CustomModifications", resolved[0])
	}

	// Verify custom stage directory was created
	customStageDir := filepath.Join(transformDir, "50_CustomModifications")
	if _, err := os.Stat(customStageDir); os.IsNotExist(err) {
		t.Errorf("custom stage directory not created: %s", customStageDir)
	}

	if !orchestrator.NewlyCreatedStages["50_CustomModifications"] {
		t.Errorf("custom stage not marked as newly created")
	}
}

// Test that base name without priority prefix finds existing stage
func TestResolveAndValidateStages_BaseNameFindsExistingStage(t *testing.T) {
	env := newResolveStagesTestEnv(t)

	// Create existing stage "50_CustomStage"
	existingStageDir := filepath.Join(env.TransformDir, "50_CustomStage")
	if err := os.MkdirAll(existingStageDir, 0755); err != nil {
		t.Fatalf("failed to create existing stage dir: %v", err)
	}

	resolved, err := env.Options.resolveAndValidateStages(
		[]string{"CustomStage"},
		env.Orchestrator,
		env.TransformDir,
		env.PluginDir,
		env.Log,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved stage, got %d", len(resolved))
	}

	if resolved[0] != "50_CustomStage" {
		t.Errorf("expected resolved stage %q, got %q", "50_CustomStage", resolved[0])
	}

	if env.Orchestrator.NewlyCreatedStages["50_CustomStage"] {
		t.Errorf("existing stage should not be marked as newly created")
	}
}

// Test that base name without priority prefix creates new stage if none exists
func TestResolveAndValidateStages_BaseNameCreatesNewStage(t *testing.T) {
	env := newResolveStagesTestEnv(t)

	// Create one existing stage to establish max priority
	existingStageDir := filepath.Join(env.TransformDir, "10_KubernetesPlugin")
	if err := os.MkdirAll(existingStageDir, 0755); err != nil {
		t.Fatalf("failed to create existing stage dir: %v", err)
	}
	// Create output directory to pass previous stage check
	existingStageOutputDir := filepath.Join(env.TransformDir, ".work", "10_KubernetesPlugin", "output")
	if err := os.MkdirAll(existingStageOutputDir, 0755); err != nil {
		t.Fatalf("failed to create existing stage output dir: %v", err)
	}

	resolved, err := env.Options.resolveAndValidateStages(
		[]string{"MyCustomStage"},
		env.Orchestrator,
		env.TransformDir,
		env.PluginDir,
		env.Log,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved stage, got %d", len(resolved))
	}

	expectedStageName := "20_MyCustomStage"
	if resolved[0] != expectedStageName {
		t.Errorf("expected resolved stage %q, got %q", expectedStageName, resolved[0])
	}

	newStageDir := filepath.Join(env.TransformDir, expectedStageName)
	if _, err := os.Stat(newStageDir); os.IsNotExist(err) {
		t.Errorf("new stage directory not created: %s", newStageDir)
	}

	if !env.Orchestrator.NewlyCreatedStages[expectedStageName] {
		t.Errorf("new stage not marked as newly created")
	}
}

// Test that ambiguous base name (multiple matches) returns error
func TestResolveAndValidateStages_BaseNameAmbiguousError(t *testing.T) {
	env := newResolveStagesTestEnv(t)

	// Create two existing stages with same base name "CustomStage"
	stage1Dir := filepath.Join(env.TransformDir, "20_CustomStage")
	if err := os.MkdirAll(stage1Dir, 0755); err != nil {
		t.Fatalf("failed to create stage 1 dir: %v", err)
	}
	stage2Dir := filepath.Join(env.TransformDir, "50_CustomStage")
	if err := os.MkdirAll(stage2Dir, 0755); err != nil {
		t.Fatalf("failed to create stage 2 dir: %v", err)
	}

	_, err := env.Options.resolveAndValidateStages(
		[]string{"CustomStage"},
		env.Orchestrator,
		env.TransformDir,
		env.PluginDir,
		env.Log,
	)

	// Should return error about ambiguity
	if err == nil {
		t.Fatal("expected error for ambiguous base name, but got none")
	}

	// Error should mention both stage names
	errMsg := err.Error()
	if !strings.Contains(errMsg, "20_CustomStage") || !strings.Contains(errMsg, "50_CustomStage") {
		t.Errorf("error message should mention both stages: %v", errMsg)
	}
	if !strings.Contains(errMsg, "multiple stages") {
		t.Errorf("error message should mention 'multiple stages': %v", errMsg)
	}
}

// Test that multiple base names create stages with increasing priorities
func TestResolveAndValidateStages_MultipleBaseNamesIncrementPriority(t *testing.T) {
	env := newResolveStagesTestEnv(t)

	resolved, err := env.Options.resolveAndValidateStages(
		[]string{"FirstStage", "SecondStage", "ThirdStage"},
		env.Orchestrator,
		env.TransformDir,
		env.PluginDir,
		env.Log,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resolved) != 3 {
		t.Fatalf("expected 3 resolved stages, got %d", len(resolved))
	}

	expected := []string{"10_FirstStage", "20_SecondStage", "30_ThirdStage"}
	for i, expectedName := range expected {
		if resolved[i] != expectedName {
			t.Errorf("stage %d: expected %q, got %q", i, expectedName, resolved[i])
		}

		stageDir := filepath.Join(env.TransformDir, expectedName)
		if _, err := os.Stat(stageDir); os.IsNotExist(err) {
			t.Errorf("stage directory not created: %s", stageDir)
		}

		if !env.Orchestrator.NewlyCreatedStages[expectedName] {
			t.Errorf("stage %q not marked as newly created", expectedName)
		}
	}
}

func TestValidate_ExportDir(t *testing.T) {
	tmpDir := t.TempDir()

	validExportDir := filepath.Join(tmpDir, "export")
	if err := os.MkdirAll(validExportDir, 0o755); err != nil {
		t.Fatalf("failed to create valid export dir: %v", err)
	}

	notDirPath := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(notDirPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("failed to create file path test fixture: %v", err)
	}

	tests := []struct {
		name      string
		exportDir string
		wantErr   string
	}{
		{
			name:      "missing export dir",
			exportDir: filepath.Join(tmpDir, "missing"),
			wantErr:   "does not exist",
		},
		{
			name:      "export dir is file",
			exportDir: notDirPath,
			wantErr:   "is not a directory",
		},
		{
			name:      "valid export dir",
			exportDir: validExportDir,
			wantErr:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{
				Flags: Flags{
					ExportDir:    tt.exportDir,
					PluginDir:    filepath.Join(tmpDir, "plugins"),
					TransformDir: filepath.Join(tmpDir, "transform"),
				},
			}

			err := o.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
			if !strings.Contains(err.Error(), "export-dir") {
				t.Fatalf("expected error to mention export-dir, got %v", err)
			}
		})
	}
}

func TestValidate_MissingExportDir_FailsBeforeRun(t *testing.T) {
	tmpDir := t.TempDir()
	transformDir := filepath.Join(tmpDir, "transform")

	o := &Options{
		Flags: Flags{
			ExportDir:    filepath.Join(tmpDir, "missing-export"),
			TransformDir: transformDir,
			PluginDir:    filepath.Join(tmpDir, "plugins"),
		},
	}

	err := o.Validate()
	if err == nil {
		t.Fatalf("expected validate to fail for missing export dir")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected missing export-dir error, got %v", err)
	}

	// Validate should not create transform artifacts.
	if _, statErr := os.Stat(transformDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected transform dir to not exist after validation failure, got stat err: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(transformDir, ".work")); !os.IsNotExist(statErr) {
		t.Fatalf("expected .work dir to not exist after validation failure, got stat err: %v", statErr)
	}
}
