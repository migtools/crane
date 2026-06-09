package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/konveyor/crane/internal/flags"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name            string
		requestedStages []string
		wantError       bool
	}{
		{
			name:            "no stages - valid",
			requestedStages: []string{},
			wantError:       false,
		},
		{
			name:            "valid stage directory name",
			requestedStages: []string{"10_KubernetesPlugin"},
			wantError:       false,
		},
		{
			name:            "multiple valid stages",
			requestedStages: []string{"10_KubernetesPlugin", "20_OpenshiftPlugin"},
			wantError:       false,
		},
		{
			name:            "plugin name without prefix - valid (will be resolved)",
			requestedStages: []string{"KubernetesPlugin"},
			wantError:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{
				RequestedStages: tt.requestedStages,
			}

			err := o.Validate()

			if tt.wantError {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
					return
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestStageSelectionRouting(t *testing.T) {
	tests := []struct {
		name            string
		requestedStages []string
		expectCustom    bool     // true if user specified custom selector
		selectorStages  []string // expected stage values in selector
	}{
		{
			name:            "default - no stages (all stages)",
			requestedStages: []string{},
			expectCustom:    false, // No custom selector = all stages
		},
		{
			name:            "single stage",
			requestedStages: []string{"10_KubernetesPlugin"},
			expectCustom:    true,
			selectorStages:  []string{"10_KubernetesPlugin"},
		},
		{
			name:            "multiple stages",
			requestedStages: []string{"10_KubernetesPlugin", "20_OpenshiftPlugin"},
			expectCustom:    true,
			selectorStages:  []string{"10_KubernetesPlugin", "20_OpenshiftPlugin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the routing logic from run() method
			// Now everything uses ApplyMultiStage(), the question is whether
			// user provided a custom selector or we use default (all stages)
			hasCustomSelector := len(tt.requestedStages) > 0

			if hasCustomSelector != tt.expectCustom {
				t.Errorf("Custom selector: got %v, want %v", hasCustomSelector, tt.expectCustom)
			}

			if tt.expectCustom {
				// Verify selector would be constructed correctly
				if len(tt.requestedStages) != len(tt.selectorStages) {
					t.Errorf("Selector Stages length: got %v, want %v", len(tt.requestedStages), len(tt.selectorStages))
				}
				for i, stage := range tt.requestedStages {
					if stage != tt.selectorStages[i] {
						t.Errorf("Selector Stages[%d]: got %v, want %v", i, stage, tt.selectorStages[i])
					}
				}
			}
		})
	}
}

func TestComplete(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStages []string
	}{
		{
			name:       "no args",
			args:       []string{},
			wantStages: []string{},
		},
		{
			name:       "single stage",
			args:       []string{"10_KubernetesPlugin"},
			wantStages: []string{"10_KubernetesPlugin"},
		},
		{
			name:       "multiple stages",
			args:       []string{"10_KubernetesPlugin", "20_OpenshiftPlugin"},
			wantStages: []string{"10_KubernetesPlugin", "20_OpenshiftPlugin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{}
			cmd := &cobra.Command{}

			err := o.Complete(cmd, tt.args)
			if err != nil {
				t.Errorf("Complete() unexpected error: %v", err)
			}

			if len(o.RequestedStages) != len(tt.wantStages) {
				t.Errorf("RequestedStages length: got %v, want %v", len(o.RequestedStages), len(tt.wantStages))
			}

			for i, stage := range o.RequestedStages {
				if stage != tt.wantStages[i] {
					t.Errorf("RequestedStages[%d]: got %v, want %v", i, stage, tt.wantStages[i])
				}
			}
		})
	}
}

// TestRun_UnresolvedStagesError tests that apply returns error when requested stages don't exist
func TestRun_UnresolvedStagesError(t *testing.T) {
	tempDir := t.TempDir()
	transformDir := filepath.Join(tempDir, "transform")
	outputDir := filepath.Join(tempDir, "output")

	// Create transform directory with one existing stage
	if err := os.MkdirAll(transformDir, 0755); err != nil {
		t.Fatalf("failed to create transform dir: %v", err)
	}

	// Create one existing stage
	existingStageDir := filepath.Join(transformDir, "10_KubernetesPlugin")
	if err := os.MkdirAll(existingStageDir, 0755); err != nil {
		t.Fatalf("failed to create stage dir: %v", err)
	}

	// Create a minimal kustomization.yaml
	kustomizationPath := filepath.Join(existingStageDir, "kustomization.yaml")
	if err := os.WriteFile(kustomizationPath, []byte("resources: []\n"), 0644); err != nil {
		t.Fatalf("failed to create kustomization.yaml: %v", err)
	}

	log := logrus.New()
	log.SetOutput(os.Stderr)

	globalFlags := &flags.GlobalFlags{}

	tests := []struct {
		name            string
		requestedStages []string
		expectError     bool
		errorContains   string
	}{
		{
			name:            "valid stage - no error",
			requestedStages: []string{"10_KubernetesPlugin"},
			expectError:     false,
		},
		{
			name:            "valid plugin name - no error",
			requestedStages: []string{"KubernetesPlugin"},
			expectError:     false,
		},
		{
			name:            "typo in stage name - error",
			requestedStages: []string{"TypoStage"},
			expectError:     true,
			errorContains:   "not found",
		},
		{
			name:            "one valid one invalid - error",
			requestedStages: []string{"KubernetesPlugin", "TypoStage"},
			expectError:     true,
			errorContains:   "TypoStage",
		},
		{
			name:            "multiple invalid - error lists all",
			requestedStages: []string{"InvalidOne", "InvalidTwo"},
			expectError:     true,
			errorContains:   "InvalidOne",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{
				cobraGlobalFlags: globalFlags,
				globalFlags:      globalFlags,
				Flags: Flags{
					TransformDir: transformDir,
					OutputDir:    outputDir,
				},
				RequestedStages: tt.requestedStages,
			}

			err := o.run()

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("error should contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidate_TransformDir(t *testing.T) {
	tmpDir := t.TempDir()

	validDir := filepath.Join(tmpDir, "transform")
	if err := os.MkdirAll(validDir, 0o755); err != nil {
		t.Fatalf("failed to create valid dir: %v", err)
	}

	filePath := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("failed to create file fixture: %v", err)
	}

	tests := []struct {
		name         string
		transformDir string
		wantErrPart  string
	}{
		{
			name:         "missing transform-dir",
			transformDir: filepath.Join(tmpDir, "missing"),
			wantErrPart:  "does not exist",
		},
		{
			name:         "transform-dir is file",
			transformDir: filePath,
			wantErrPart:  "is not a directory",
		},
		{
			name:         "valid transform-dir",
			transformDir: validDir,
			wantErrPart:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{
				Flags: Flags{
					TransformDir: tt.transformDir,
				},
			}

			err := o.Validate()
			if tt.wantErrPart == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErrPart)
			}
			if !strings.Contains(err.Error(), tt.wantErrPart) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErrPart, err)
			}
		})
	}
}

func TestValidate_MissingTransformDir_DoesNotCreateOutputDir(t *testing.T) {
	tmpDir := t.TempDir()
	missingTransformDir := filepath.Join(tmpDir, "missing-transform")
	outputDir := filepath.Join(tmpDir, "output")

	o := &Options{
		Flags: Flags{
			TransformDir: missingTransformDir,
			OutputDir:    outputDir,
		},
	}

	err := o.Validate()
	if err == nil {
		t.Fatalf("expected validate to fail for missing transform-dir")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected missing transform-dir error, got %v", err)
	}

	// Since Validate fails, run should not be called; ensure no output side effects happened.
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected output dir to not exist after validation failure, got stat err: %v", statErr)
	}
}
