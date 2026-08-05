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
	"sigs.k8s.io/yaml"
)

func TestNewResources_DirectoryStructure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "newresource-structure-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	transformDir := filepath.Join(tmpDir, "transform")

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	opts := file.PathOpts{
		TransformDir: transformDir,
	}

	// Original resource (goes to input/)
	originalResource := &unstructured.Unstructured{}
	originalResource.SetKind("BuildConfig")
	originalResource.SetAPIVersion("build.openshift.io/v1")
	originalResource.SetName("my-app")
	originalResource.SetNamespace("default")

	// New resource skeleton (goes to new/)
	newSkeleton := &unstructured.Unstructured{}
	newSkeleton.SetKind("Build")
	newSkeleton.SetAPIVersion("shipwright.io/v1beta1")
	newSkeleton.SetName("my-app-build")
	newSkeleton.SetNamespace("default")

	// Build a patch that adds spec (simulating SplitNewResourceToSkeletonAndPatch output)
	fullResource := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "shipwright.io/v1beta1",
			"kind":       "Build",
			"metadata": map[string]interface{}{
				"name":      "my-app-build",
				"namespace": "default",
				"labels":    map[string]interface{}{"app": "my-app"},
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type": "Git",
					"git":  map[string]interface{}{"url": "https://github.com/example/repo"},
				},
			},
		},
	}
	_, newPatch, err := cranelib.SplitNewResourceToSkeletonAndPatch(fullResource)
	if err != nil {
		t.Fatalf("SplitNewResourceToSkeletonAndPatch failed: %v", err)
	}

	artifacts := []StageArtifact{
		{
			TransformArtifact: cranelib.TransformArtifact{
				Resource:     *originalResource,
				HaveWhiteOut: true,
				Patches:      nil,
				IgnoredOps:   []cranelib.IgnoredOperation{},
				Target:       cranelib.DeriveTargetFromResource(*originalResource),
				PluginName:   "BuildConfigPlugin",
			},
		},
		{
			TransformArtifact: cranelib.TransformArtifact{
				Resource:     *newSkeleton,
				HaveWhiteOut: false,
				Patches:      newPatch,
				IgnoredOps:   []cranelib.IgnoredOperation{},
				Target:       cranelib.DeriveTargetFromResource(*newSkeleton),
				PluginName:   "BuildConfigPlugin",
			},
			IsNewResource: true,
		},
	}

	writer := NewKustomizeWriter(opts, "10_BuildConfigPlugin", logger)
	if err := writer.WriteStage(artifacts, true); err != nil {
		t.Fatalf("WriteStage failed: %v", err)
	}

	stageDir := filepath.Join(transformDir, "10_BuildConfigPlugin")

	// 1. input/ should contain the original BuildConfig
	inputDir := filepath.Join(stageDir, "input")
	inputFiles, _ := os.ReadDir(inputDir)
	hasOriginal := false
	for _, f := range inputFiles {
		if strings.Contains(f.Name(), "BuildConfig") && strings.Contains(f.Name(), "my-app") {
			hasOriginal = true
		}
	}
	if !hasOriginal {
		t.Errorf("Expected BuildConfig in input/")
	}

	// 2. new/ should contain the Build skeleton
	newDir := filepath.Join(stageDir, "new")
	newFiles, _ := os.ReadDir(newDir)
	hasBuildSkeleton := false
	var buildSkeletonFile string
	for _, f := range newFiles {
		if strings.Contains(f.Name(), "Build") && strings.Contains(f.Name(), "my-app-build") {
			hasBuildSkeleton = true
			buildSkeletonFile = filepath.Join(newDir, f.Name())
		}
	}
	if !hasBuildSkeleton {
		t.Errorf("Expected Build skeleton in new/")
	}

	// 3. Verify skeleton content is minimal (no spec, no labels, no annotations)
	if buildSkeletonFile != "" {
		content, err := os.ReadFile(buildSkeletonFile)
		if err != nil {
			t.Fatalf("Failed to read skeleton file: %v", err)
		}

		// Parse and verify structure
		var skelObj map[string]interface{}
		if err := yaml.Unmarshal(content, &skelObj); err != nil {
			t.Fatalf("Failed to parse skeleton YAML: %v", err)
		}
		if _, hasSpec := skelObj["spec"]; hasSpec {
			t.Errorf("Parsed skeleton should not have spec")
		}
		meta, _ := skelObj["metadata"].(map[string]interface{})
		if _, hasLabels := meta["labels"]; hasLabels {
			t.Errorf("Skeleton metadata should not have labels")
		}
		if _, hasAnnotations := meta["annotations"]; hasAnnotations {
			t.Errorf("Skeleton metadata should not have annotations")
		}
	}

	// 4. patches/ should contain a patch file for the new Build
	patchesDir := filepath.Join(stageDir, "patches")
	patchFiles, _ := os.ReadDir(patchesDir)
	hasBuildPatch := false
	for _, f := range patchFiles {
		if strings.Contains(f.Name(), "Build") && strings.Contains(f.Name(), "my-app-build") {
			hasBuildPatch = true

			// Verify patch content has add operations
			patchContent, _ := os.ReadFile(filepath.Join(patchesDir, f.Name()))
			patchStr := string(patchContent)
			if !strings.Contains(patchStr, "op: add") {
				t.Errorf("Patch should contain 'op: add' operations, got:\n%s", patchStr)
			}
			if !strings.Contains(patchStr, "/spec") {
				t.Errorf("Patch should add /spec, got:\n%s", patchStr)
			}
		}
	}
	if !hasBuildPatch {
		t.Errorf("Expected patch file for Build in patches/")
	}

	// 5. kustomization.yaml should reference new/ for the Build
	kustomizationContent, err := os.ReadFile(filepath.Join(stageDir, "kustomization.yaml"))
	if err != nil {
		t.Fatalf("Failed to read kustomization.yaml: %v", err)
	}
	kStr := string(kustomizationContent)

	if !strings.Contains(kStr, "new/Build") {
		t.Errorf("kustomization.yaml should reference new/ for Build, got:\n%s", kStr)
	}
	// BuildConfig whiteout should be commented out
	if !strings.Contains(kStr, "# - input/BuildConfig") {
		t.Errorf("kustomization.yaml should have commented-out whiteout for BuildConfig, got:\n%s", kStr)
	}

	t.Log("Directory structure verified:")
	t.Log("  input/   → BuildConfig (whiteout)")
	t.Log("  new/     → Build skeleton (minimal)")
	t.Log("  patches/ → Build patch (add spec, labels)")
	t.Log("  kustomization.yaml → references new/ and patches/")
}

func TestNewResources_KustomizeBuildProducesCompleteResource(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "newresource-kustomize-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	transformDir := filepath.Join(tmpDir, "transform")

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	opts := file.PathOpts{
		TransformDir: transformDir,
	}

	// Simulate full orchestrator flow: plugin returns complete resource,
	// orchestrator splits to skeleton+patch, writer writes to disk
	fullResource := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "shipwright.io/v1beta1",
			"kind":       "Build",
			"metadata": map[string]interface{}{
				"name":      "my-app-build",
				"namespace": "default",
				"labels":    map[string]interface{}{"app": "my-app"},
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type": "Git",
					"git":  map[string]interface{}{"url": "https://github.com/example/repo"},
				},
				"output": map[string]interface{}{
					"image": "quay.io/example/my-app:latest",
				},
			},
		},
	}

	skeleton, patch, err := cranelib.SplitNewResourceToSkeletonAndPatch(fullResource)
	if err != nil {
		t.Fatalf("Split failed: %v", err)
	}

	artifacts := []StageArtifact{
		{
			TransformArtifact: cranelib.TransformArtifact{
				Resource:     skeleton,
				HaveWhiteOut: false,
				Patches:      patch,
				IgnoredOps:   []cranelib.IgnoredOperation{},
				Target:       cranelib.DeriveTargetFromResource(skeleton),
				PluginName:   "TestPlugin",
			},
			IsNewResource: true,
		},
	}

	writer := NewKustomizeWriter(opts, "10_TestPlugin", logger)
	if err := writer.WriteStage(artifacts, true); err != nil {
		t.Fatalf("WriteStage failed: %v", err)
	}

	// Run kustomize build on the stage
	o := &Orchestrator{Log: logger}
	stageDir := filepath.Join(transformDir, "10_TestPlugin")
	resources, err := o.applyStageTransforms(stageDir)
	if err != nil {
		t.Fatalf("kustomize build failed: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("Expected 1 resource from kustomize build, got %d", len(resources))
	}

	result := resources[0]
	if result.GetKind() != "Build" {
		t.Errorf("Result kind: got %q", result.GetKind())
	}
	if result.GetName() != "my-app-build" {
		t.Errorf("Result name: got %q", result.GetName())
	}

	// Verify the full resource was reconstructed
	labels := result.GetLabels()
	if labels["app"] != "my-app" {
		t.Errorf("Result should have label app=my-app, got %v", labels)
	}

	img, _, _ := unstructured.NestedString(result.Object, "spec", "output", "image")
	if img != "quay.io/example/my-app:latest" {
		t.Errorf("Result spec.output.image: got %q", img)
	}

	gitURL, _, _ := unstructured.NestedString(result.Object, "spec", "source", "git", "url")
	if gitURL != "https://github.com/example/repo" {
		t.Errorf("Result spec.source.git.url: got %q", gitURL)
	}

	t.Log("kustomize build successfully produced complete resource from skeleton + patches")
}
