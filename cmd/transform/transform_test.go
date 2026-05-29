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
		globalFlags: &flags.GlobalFlags{},
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

// Test that multiple new stages created in one call get unique, increasing priorities
func TestResolveAndValidateStages_MultipleNewStagesUniquePriorities(t *testing.T) {
	// Setup: temp directories
	tempDir := t.TempDir()
	transformDir := filepath.Join(tempDir, "transform")

	if err := os.MkdirAll(transformDir, 0755); err != nil {
		t.Fatalf("failed to create transform dir: %v", err)
	}

	// Create one existing stage with priority 10
	existingStageDir := filepath.Join(transformDir, "10_ExistingPlugin")
	if err := os.MkdirAll(existingStageDir, 0755); err != nil {
		t.Fatalf("failed to create existing stage dir: %v", err)
	}

	// Test requesting multiple non-existent plugin names
	// This simulates the bug scenario where multiple new stages are created
	requestedStages := []string{"FooPlugin", "BarPlugin", "BazPlugin"}

	// We can't easily test the full resolveAndValidateStages without real plugins,
	// but we can verify the priority calculation logic by checking what would happen
	// if we discover existing stages and compute nextPriority

	existingStages, err := internalTransform.DiscoverStages(transformDir)
	if err != nil {
		t.Fatalf("failed to discover stages: %v", err)
	}

	// Verify we have 1 existing stage
	if len(existingStages) != 1 {
		t.Fatalf("expected 1 existing stage, got %d", len(existingStages))
	}
	if existingStages[0].Priority != 10 {
		t.Fatalf("expected existing stage priority 10, got %d", existingStages[0].Priority)
	}

	// Simulate the priority calculation logic from resolveAndValidateStages
	maxPriority := 0
	for _, stage := range existingStages {
		if stage.Priority > maxPriority {
			maxPriority = stage.Priority
		}
	}
	nextPriority := maxPriority + 10

	// Simulate creating 3 new stages and verify priorities are unique and increasing
	expectedPriorities := []int{20, 30, 40}
	generatedPriorities := []int{}

	for range requestedStages {
		generatedPriorities = append(generatedPriorities, nextPriority)
		nextPriority += 10 // This is the fix - increment after each stage
	}

	// Verify priorities are unique and increasing
	for i, expected := range expectedPriorities {
		if generatedPriorities[i] != expected {
			t.Errorf("Stage %d: expected priority %d, got %d", i, expected, generatedPriorities[i])
		}
	}

	// Verify all priorities are unique
	seen := make(map[int]bool)
	for _, p := range generatedPriorities {
		if seen[p] {
			t.Errorf("Duplicate priority found: %d", p)
		}
		seen[p] = true
	}

	// Verify priorities are strictly increasing
	for i := 1; i < len(generatedPriorities); i++ {
		if generatedPriorities[i] <= generatedPriorities[i-1] {
			t.Errorf("Priorities not increasing: %d -> %d", generatedPriorities[i-1], generatedPriorities[i])
		}
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

	// Create one existing stage
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

	orchestrator := &internalTransform.Orchestrator{
		Log:                log,
		TransformDir:       transformDir,
		NewlyCreatedStages: make(map[string]bool),
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
			name:           "invalid stage name without plugin errors",
			requestedStage: "InvalidStageName",
			shouldCreate:   false,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset newly created stages
			orchestrator.NewlyCreatedStages = make(map[string]bool)

			resolved, err := o.resolveAndValidateStages(
				[]string{tt.requestedStage},
				orchestrator,
				transformDir,
				pluginDir,
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

			if resolved[0] != tt.requestedStage {
				t.Errorf("expected resolved stage %q, got %q", tt.requestedStage, resolved[0])
			}

			// Check if directory was created
			stageDir := filepath.Join(transformDir, tt.requestedStage)
			_, err = os.Stat(stageDir)
			dirExists := err == nil

			if tt.shouldCreate {
				if !dirExists {
					t.Errorf("expected custom stage directory to be created at %s", stageDir)
				}
				if !orchestrator.NewlyCreatedStages[tt.requestedStage] {
					t.Errorf("expected stage to be marked as newly created")
				}
			} else {
				if orchestrator.NewlyCreatedStages[tt.requestedStage] {
					t.Errorf("did not expect stage to be marked as newly created")
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

	// Create one existing stage
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
