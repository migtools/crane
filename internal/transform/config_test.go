package transform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Valid config with known-safe stage tokens should pass validation unchanged.
func TestValidateConfig_Valid(t *testing.T) {
	cfg := &ConfigFile{
		Stages: []string{"KubernetesPlugin", "CustomStage"},
	}

	err := ValidateConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.Stages[0] != "KubernetesPlugin" || cfg.Stages[1] != "CustomStage" {
		t.Fatalf("expected trimmed stages to be preserved, got %#v", cfg.Stages)
	}
}

// Duplicate stage tokens should be rejected to avoid ambiguous stage layouts.
func TestValidateConfig_DuplicateStages(t *testing.T) {
	cfg := &ConfigFile{
		Stages: []string{"KubernetesPlugin", "KubernetesPlugin"},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatalf("expected duplicate stage error, got nil")
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

// Stage tokens with unsafe characters should fail validation.
func TestValidateConfig_InvalidCharacters(t *testing.T) {
	cfg := &ConfigFile{
		Stages: []string{"KubernetesPlugin", "../bad"},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatalf("expected invalid character error, got nil")
	}
}

// Empty stages list should fail validation because at least one stage is required.
func TestValidateConfig_EmptyStages(t *testing.T) {
	cfg := &ConfigFile{
		Stages: []string{},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatalf("expected empty stages error, got nil")
	}
}

// Unknown top-level keys should fail decoding in strict mode.
func TestLoadConfig_UnknownFieldFails(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "bad-config.yaml")

	content := []byte(`stages:
  - KubernetesPlugin
description: not-supported-yet
`)
	if err := os.WriteFile(cfgPath, content, 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := LoadConfig(cfgPath)
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
