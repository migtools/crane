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
		{
			name:       "valid instructions",
			cfg:        &InstructionsFile{Stages: []StageEntry{{Name: "KubernetesPlugin"}, {Name: "CustomStage"}}},
			wantErr:    false,
			wantStages: []string{"KubernetesPlugin", "CustomStage"},
		},
		{
			name:       "valid instructions file trims stage names",
			cfg:        &InstructionsFile{Stages: []StageEntry{{Name: " KubernetesPlugin "}, {Name: "  CustomStage\t"}}},
			wantErr:    false,
			wantStages: []string{"KubernetesPlugin", "CustomStage"},
		},
		{
			name:    "duplicate stages in instructions file",
			cfg:     &InstructionsFile{Stages: []StageEntry{{Name: "KubernetesPlugin"}, {Name: "KubernetesPlugin"}}},
			wantErr: true,
		},
		{
			name:    "invalid characters in instructions file",
			cfg:     &InstructionsFile{Stages: []StageEntry{{Name: "KubernetesPlugin"}, {Name: "../bad"}}},
			wantErr: true,
		},
		{
			name:    "empty stages list in instructions file",
			cfg:     &InstructionsFile{Stages: []StageEntry{}},
			wantErr: true,
		},
		{
			name:    "nil instructions file",
			cfg:     nil,
			wantErr: true,
		},
		{
			name:    "empty stage entry in instructions file",
			cfg:     &InstructionsFile{Stages: []StageEntry{{Name: "KubernetesPlugin"}, {Name: "   "}}},
			wantErr: true,
		},
		{
			name: "valid stage with optionals",
			cfg: &InstructionsFile{Stages: []StageEntry{
				{Name: "KubernetesPlugin", Optionals: map[string]string{"registry-replacement": "docker.io=quay.io"}},
			}},
			wantErr:    false,
			wantStages: []string{"KubernetesPlugin"},
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
				names := tt.cfg.StageNames()
				if len(names) != len(tt.wantStages) {
					t.Fatalf("stages length mismatch: got %d want %d", len(names), len(tt.wantStages))
				}
				for i := range tt.wantStages {
					if names[i] != tt.wantStages[i] {
						t.Fatalf("at index %d: got %q want %q", i, names[i], tt.wantStages[i])
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

// Mixed-list YAML format: plain strings and objects can be freely intermixed.
func TestLoadInstructions_MixedListFormat(t *testing.T) {
	tmpDir := t.TempDir()
	instructionsFilePath := filepath.Join(tmpDir, "mixed-instructions.yaml")

	content := []byte(`stages:
  - KubernetesPlugin
  - name: RegistryPlugin
    optionals:
      registry-replacement: "docker.io=quay.io"
  - CustomEdits
`)
	if err := os.WriteFile(instructionsFilePath, content, 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadInstructions(instructionsFilePath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(cfg.Stages) != 3 {
		t.Fatalf("expected 3 stages, got %d", len(cfg.Stages))
	}

	// First stage: plain string
	if cfg.Stages[0].Name != "KubernetesPlugin" {
		t.Errorf("stage 0 name: expected %q, got %q", "KubernetesPlugin", cfg.Stages[0].Name)
	}
	if len(cfg.Stages[0].Optionals) != 0 {
		t.Errorf("stage 0 optionals: expected empty, got %v", cfg.Stages[0].Optionals)
	}

	// Second stage: object with optionals
	if cfg.Stages[1].Name != "RegistryPlugin" {
		t.Errorf("stage 1 name: expected %q, got %q", "RegistryPlugin", cfg.Stages[1].Name)
	}
	if cfg.Stages[1].Optionals["registry-replacement"] != "docker.io=quay.io" {
		t.Errorf("stage 1 optionals: expected registry-replacement=docker.io=quay.io, got %v", cfg.Stages[1].Optionals)
	}

	// Third stage: plain string
	if cfg.Stages[2].Name != "CustomEdits" {
		t.Errorf("stage 2 name: expected %q, got %q", "CustomEdits", cfg.Stages[2].Name)
	}
}

// Object-only stages list should also work.
func TestLoadInstructions_AllObjectFormat(t *testing.T) {
	tmpDir := t.TempDir()
	instructionsFilePath := filepath.Join(tmpDir, "all-object-instructions.yaml")

	content := []byte(`stages:
  - name: KubernetesPlugin
    optionals:
      strip-default-rbac: "false"
  - name: CustomEdits
`)
	if err := os.WriteFile(instructionsFilePath, content, 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadInstructions(instructionsFilePath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(cfg.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(cfg.Stages))
	}

	if cfg.Stages[0].Optionals["strip-default-rbac"] != "false" {
		t.Errorf("stage 0 optionals: expected strip-default-rbac=false, got %v", cfg.Stages[0].Optionals)
	}
	if len(cfg.Stages[1].Optionals) != 0 {
		t.Errorf("stage 1 optionals: expected empty, got %v", cfg.Stages[1].Optionals)
	}
}

// Backward compatibility: plain string list should still work.
func TestLoadInstructions_BackwardCompatibleStringList(t *testing.T) {
	tmpDir := t.TempDir()
	instructionsFilePath := filepath.Join(tmpDir, "string-instructions.yaml")

	content := []byte(`stages:
  - KubernetesPlugin
  - CustomStage
`)
	if err := os.WriteFile(instructionsFilePath, content, 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadInstructions(instructionsFilePath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	names := cfg.StageNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(names))
	}
	if names[0] != "KubernetesPlugin" || names[1] != "CustomStage" {
		t.Fatalf("unexpected stage names: %v", names)
	}
}

// Unknown fields in stage objects should fail.
func TestLoadInstructions_UnknownStageFieldFails(t *testing.T) {
	tmpDir := t.TempDir()
	instructionsFilePath := filepath.Join(tmpDir, "bad-stage-field.yaml")

	content := []byte(`stages:
  - name: KubernetesPlugin
    unknown-field: value
`)
	if err := os.WriteFile(instructionsFilePath, content, 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := LoadInstructions(instructionsFilePath)
	if err == nil {
		t.Fatalf("expected error for unknown stage field, got nil")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

// StageOptionals helper should return only stages with optionals.
func TestInstructionsFile_StageOptionals(t *testing.T) {
	cfg := &InstructionsFile{
		Stages: []StageEntry{
			{Name: "KubernetesPlugin"},
			{Name: "RegistryPlugin", Optionals: map[string]string{"registry-replacement": "docker.io=quay.io"}},
			{Name: "CustomEdits"},
		},
	}

	optionals, err := cfg.StageOptionals()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(optionals) != 1 {
		t.Fatalf("expected 1 entry in StageOptionals, got %d", len(optionals))
	}
	if optionals["RegistryPlugin"]["registry-replacement"] != "docker.io=quay.io" {
		t.Errorf("unexpected optionals for RegistryPlugin: %v", optionals["RegistryPlugin"])
	}
}

// StageOptionals should lowercase keys for viper compatibility.
func TestInstructionsFile_StageOptionals_LowercasesKeys(t *testing.T) {
	cfg := &InstructionsFile{
		Stages: []StageEntry{
			{Name: "RegistryPlugin", Optionals: map[string]string{
				"Registry-Replacement": "docker.io=quay.io",
				"Strip-Default-RBAC":   "true",
			}},
		},
	}

	optionals, err := cfg.StageOptionals()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := optionals["RegistryPlugin"]
	if got["registry-replacement"] != "docker.io=quay.io" {
		t.Errorf("expected lowercased key registry-replacement, got keys: %v", got)
	}
	if got["strip-default-rbac"] != "true" {
		t.Errorf("expected lowercased key strip-default-rbac, got keys: %v", got)
	}
	if _, exists := got["Registry-Replacement"]; exists {
		t.Errorf("original mixed-case key should not be present after lowercasing")
	}
}

// StageOptionals should reject case-insensitive duplicate keys.
func TestInstructionsFile_StageOptionals_RejectsCaseCollision(t *testing.T) {
	cfg := &InstructionsFile{
		Stages: []StageEntry{
			{Name: "RegistryPlugin", Optionals: map[string]string{
				"Registry-Replacement": "docker.io=quay.io",
				"registry-replacement": "gcr.io=ghcr.io",
			}},
		},
	}

	_, err := cfg.StageOptionals()
	if err == nil {
		t.Fatalf("expected error for case-insensitive duplicate key, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate optional key") {
		t.Fatalf("expected duplicate key error, got %v", err)
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
