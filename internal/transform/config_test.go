package transform

import "testing"

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

func TestValidateConfig_DuplicateStages(t *testing.T) {
	cfg := &ConfigFile{
		Stages: []string{"KubernetesPlugin", "KubernetesPlugin"},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatalf("expected duplicate stage error, got nil")
	}
}

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
