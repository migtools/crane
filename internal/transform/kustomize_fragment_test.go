package transform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	cranelib "github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane/internal/file"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Instructions file: per-stage kustomize fragment is parsed and exposed via StageKustomize.
func TestLoadInstructions_KustomizeFragment(t *testing.T) {
	tmpDir := t.TempDir()
	instructionsFilePath := filepath.Join(tmpDir, "kustomize-instructions.yaml")

	content := []byte(`stages:
  - name: KubernetesPlugin
    kustomize:
      namespace: dest-ns
      commonLabels:
        app: crane
  - name: CustomEdits
`)
	if err := os.WriteFile(instructionsFilePath, content, 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadInstructions(instructionsFilePath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	frags := cfg.StageKustomize()
	if len(frags) != 1 {
		t.Fatalf("expected 1 stage with kustomize, got %d", len(frags))
	}
	frag := frags["KubernetesPlugin"]
	if frag["namespace"] != "dest-ns" {
		t.Errorf("namespace: got %v", frag["namespace"])
	}
	if _, ok := frag["commonLabels"].(map[string]interface{}); !ok {
		t.Errorf("commonLabels: expected map, got %T", frag["commonLabels"])
	}
}

// Unknown-field error message mentions the kustomize field as supported.
func TestLoadInstructions_UnknownStageFieldMentionsKustomize(t *testing.T) {
	tmpDir := t.TempDir()
	instructionsFilePath := filepath.Join(tmpDir, "bad.yaml")

	content := []byte(`stages:
  - name: KubernetesPlugin
    bogus: value
`)
	if err := os.WriteFile(instructionsFilePath, content, 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := LoadInstructions(instructionsFilePath)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "kustomize") {
		t.Fatalf("expected error to mention kustomize, got %v", err)
	}
}

func TestValidateStageKustomizeFragments(t *testing.T) {
	stages := []Stage{
		{PluginName: "KubernetesPlugin", DirName: "10_KubernetesPlugin"},
		{PluginName: "CustomEdits", DirName: "20_CustomEdits"},
	}

	t.Run("known stage passes", func(t *testing.T) {
		o := &Orchestrator{
			StageKustomizeFragments: map[string]map[string]interface{}{
				"KubernetesPlugin": {"namespace": "ns"},
			},
		}
		if err := o.validateStageKustomizeFragments(stages); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unknown stage fails", func(t *testing.T) {
		o := &Orchestrator{
			StageKustomizeFragments: map[string]map[string]interface{}{
				"NopePlugin": {"namespace": "ns"},
			},
		}
		err := o.validateStageKustomizeFragments(stages)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "NopePlugin") {
			t.Fatalf("expected error to mention NopePlugin, got %v", err)
		}
	})

	t.Run("empty fragments pass", func(t *testing.T) {
		o := &Orchestrator{}
		if err := o.validateStageKustomizeFragments(stages); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// Writer integration: with a fragment, the generated kustomization.yaml contains
// the merged fields; without a fragment it stays free of them.
func TestWriteStage_KustomizeFragmentMerged(t *testing.T) {
	tmpDir := t.TempDir()
	transformDir := filepath.Join(tmpDir, "transform")
	stageName := "10_test"

	resource := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test-cm",
				"namespace": "default",
			},
			"data": map[string]interface{}{"k": "v"},
		},
	}

	artifact := StageArtifact{TransformArtifact: cranelib.TransformArtifact{
		Resource:     resource,
		HaveWhiteOut: false,
		Target:       cranelib.DeriveTargetFromResource(resource),
		PluginName:   "test-plugin",
	}}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	opts := file.PathOpts{TransformDir: transformDir}

	writer := NewKustomizeWriter(opts, stageName, logger)
	writer.kustomizeFragment = map[string]interface{}{
		"namespace":    "dest-ns",
		"commonLabels": map[string]interface{}{"app": "crane"},
		"resources":    []interface{}{"extra/manual.yaml"},
	}

	if err := writer.WriteStage([]StageArtifact{artifact}, true); err != nil {
		t.Fatalf("WriteStage failed: %v", err)
	}

	kustomizationPath := filepath.Join(transformDir, stageName, "kustomization.yaml")
	data, err := os.ReadFile(kustomizationPath)
	if err != nil {
		t.Fatalf("failed to read kustomization.yaml: %v", err)
	}
	content := string(data)

	for _, want := range []string{"namespace: dest-ns", "commonLabels", "app: crane", "extra/manual.yaml"} {
		if !strings.Contains(content, want) {
			t.Errorf("kustomization.yaml missing %q:\n%s", want, content)
		}
	}
	// apiVersion/kind must remain the generated kustomize ones.
	if !strings.Contains(content, "kind: Kustomization") {
		t.Errorf("kustomization.yaml lost kind: Kustomization:\n%s", content)
	}
}

// Regression: without a fragment the writer output must not contain fragment-only fields.
func TestWriteStage_NoFragmentUnchanged(t *testing.T) {
	tmpDir := t.TempDir()
	transformDir := filepath.Join(tmpDir, "transform")
	stageName := "10_test"

	resource := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test-cm",
				"namespace": "default",
			},
		},
	}

	artifact := StageArtifact{TransformArtifact: cranelib.TransformArtifact{
		Resource:   resource,
		Target:     cranelib.DeriveTargetFromResource(resource),
		PluginName: "test-plugin",
	}}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	opts := file.PathOpts{TransformDir: transformDir}

	writer := NewKustomizeWriter(opts, stageName, logger)
	if err := writer.WriteStage([]StageArtifact{artifact}, true); err != nil {
		t.Fatalf("WriteStage failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(transformDir, stageName, "kustomization.yaml"))
	if err != nil {
		t.Fatalf("failed to read kustomization.yaml: %v", err)
	}
	if strings.Contains(string(data), "commonLabels") {
		t.Errorf("unexpected fragment field in no-fragment output:\n%s", data)
	}
}
