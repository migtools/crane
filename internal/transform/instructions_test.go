package transform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateInstructions(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *InstructionsFile
		wantErr    bool
		wantStages []string
	}{
		// Valid instructions remains unchanged.
		{
			name:       "valid instructions",
			cfg:        &InstructionsFile{Stages: []string{"KubernetesPlugin", "CustomStage"}},
			wantErr:    false,
			wantStages: []string{"KubernetesPlugin", "CustomStage"},
		},
		// Whitespace around stage names is trimmed.
		{
			name:       "valid instructions file trims stage names",
			cfg:        &InstructionsFile{Stages: []string{" KubernetesPlugin ", "  CustomStage\t"}},
			wantErr:    false,
			wantStages: []string{"KubernetesPlugin", "CustomStage"},
		},
		// Duplicate stage names are rejected.
		{
			name:    "duplicate stages in instructions file",
			cfg:     &InstructionsFile{Stages: []string{"KubernetesPlugin", "KubernetesPlugin"}},
			wantErr: true,
		},
		// Unsafe characters are rejected.
		{
			name:    "invalid characters in instructions file",
			cfg:     &InstructionsFile{Stages: []string{"KubernetesPlugin", "../bad"}},
			wantErr: true,
		},
		// At least one stage is required.
		{
			name:    "empty stages list in instructions file",
			cfg:     &InstructionsFile{Stages: []string{}},
			wantErr: true,
		},
		// Nil config pointer is invalid.
		{
			name:    "nil instructions file",
			cfg:     nil,
			wantErr: true,
		},
		// Blank stage entries are invalid.
		{
			name:    "empty stage entry in instructions file",
			cfg:     &InstructionsFile{Stages: []string{"KubernetesPlugin", "   "}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateInstructions(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if len(tt.wantStages) > 0 {
				if len(tt.cfg.Stages) != len(tt.wantStages) {
					t.Fatalf("stages length mismatch: got %d want %d", len(tt.cfg.Stages), len(tt.wantStages))
				}
				for i := range tt.wantStages {
					if tt.cfg.Stages[i] != tt.wantStages[i] {
						t.Fatalf("at index %d: got %q want %q", i, tt.cfg.Stages[i], tt.wantStages[i])
					}
				}
			}
		})
	}
}

// Stage directory names should be generated deterministically by list order.
func TestGenerateStageDirNames(t *testing.T) {
	got := GenerateStageDirNames([]string{"KubernetesPlugin", "OpenshiftPlugin", "CustomStage"})
	want := []string{"10_KubernetesPlugin", "20_OpenshiftPlugin", "30_CustomStage"}

	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("at index %d: got %q want %q", i, got[i], want[i])
		}
	}
}

// Unknown top-level keys should fail decoding in strict mode.
func TestLoadInstructions_UnknownFieldFails(t *testing.T) {
	tmpDir := t.TempDir()
	instructionsFilePath := filepath.Join(tmpDir, "bad-instructions.yaml")

	content := []byte(`stages:
  - KubernetesPlugin
description: not-supported-yet
`)
	if err := os.WriteFile(instructionsFilePath, content, 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := LoadInstructions(instructionsFilePath)
	if err == nil {
		t.Fatalf("expected error for unknown field, got nil")
	}
	if !strings.Contains(err.Error(), `unknown field "description"`) {
		t.Fatalf("expected unknown field detail in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "supported top-level keys: stages") {
		t.Fatalf("expected supported keys guidance in error, got %v", err)
	}
}

// Multiple YAML documents should be rejected for instructions file input.
func TestLoadInstructions_MultipleDocumentsFails(t *testing.T) {
	tmpDir := t.TempDir()
	instructionsFilePath := filepath.Join(tmpDir, "multi-doc-instructions.yaml")

	content := []byte(`stages:
  - KubernetesPlugin
---
stages:
  - CustomStage
`)
	if err := os.WriteFile(instructionsFilePath, content, 0o600); err != nil {
		t.Fatalf("failed to write test instructions file: %v", err)
	}

	_, err := LoadInstructions(instructionsFilePath)
	if err == nil {
		t.Fatalf("expected error for multi-document instructions file, got nil")
	}
	if !strings.Contains(err.Error(), "only a single YAML document is allowed") {
		t.Fatalf("expected single-document guidance in error, got %v", err)
	}
}

// Root YAML must be a mapping with top-level stages key, not a sequence.
func TestLoadInstructions_RootSequenceFailsWithFriendlyMessage(t *testing.T) {
	tmpDir := t.TempDir()
	instructionsFilePath := filepath.Join(tmpDir, "root-seq-instructions.yaml")

	content := []byte(`- KubernetesPlugin
- CustomStage
`)
	if err := os.WriteFile(instructionsFilePath, content, 0o600); err != nil {
		t.Fatalf("failed to write test instructions file: %v", err)
	}

	_, err := LoadInstructions(instructionsFilePath)
	if err == nil {
		t.Fatalf("expected error for root sequence instructions file, got nil")
	}
	if !strings.Contains(err.Error(), `expected a mapping with top-level key "stages"`) {
		t.Fatalf("expected root mapping guidance in error, got %v", err)
	}
}
