package transform

import (
	"strings"
	"testing"

	jsonpatch "github.com/evanphx/json-patch"
	cranelib "github.com/konveyor/crane-lib/transform"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestTransformResources_SingleNewResource(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	o := &Orchestrator{
		Log:           logger,
		OptionalFlags: make(map[string]string),
	}

	buildConfig := &unstructured.Unstructured{}
	buildConfig.SetKind("BuildConfig")
	buildConfig.SetAPIVersion("build.openshift.io/v1")
	buildConfig.SetName("my-app")
	buildConfig.SetNamespace("default")

	mockPlugin := &mockNewResourcePlugin{
		name: "BuildConfigPlugin",
		newResources: []unstructured.Unstructured{
			{
				Object: map[string]interface{}{
					"apiVersion": "shipwright.io/v1beta1",
					"kind":       "Build",
					"metadata": map[string]interface{}{
						"name":      "my-app-build",
						"namespace": "default",
					},
					"spec": map[string]interface{}{
						"source": map[string]interface{}{
							"type": "Git",
							"git":  map[string]interface{}{"url": "https://github.com/example/repo"},
						},
					},
				},
			},
		},
	}

	stage := Stage{
		DirName:    "10_BuildConfigPlugin",
		Priority:   10,
		PluginName: "BuildConfigPlugin",
	}

	artifacts, err := o.transformResources(stage, mockPlugin, []unstructured.Unstructured{*buildConfig})
	if err != nil {
		t.Fatalf("transformResources failed: %v", err)
	}

	// 2 artifacts: original BuildConfig + new Build skeleton
	if len(artifacts) != 2 {
		t.Fatalf("Expected 2 artifacts (original + new), got %d", len(artifacts))
	}

	// First: original BuildConfig
	if artifacts[0].Resource.GetKind() != "BuildConfig" {
		t.Errorf("First artifact should be BuildConfig, got %s", artifacts[0].Resource.GetKind())
	}

	// Second: new Build skeleton with patches
	newArt := artifacts[1]
	if newArt.Resource.GetKind() != "Build" {
		t.Errorf("Second artifact should be Build, got %s", newArt.Resource.GetKind())
	}
	if newArt.Resource.GetName() != "my-app-build" {
		t.Errorf("New Build name: got %s", newArt.Resource.GetName())
	}
	if newArt.HaveWhiteOut {
		t.Errorf("New resource should not be whiteout")
	}

	// Skeleton should NOT have spec (it's in the patch)
	if _, exists := newArt.Resource.Object["spec"]; exists {
		t.Errorf("Skeleton should not have spec (should be in patch)")
	}

	// Should be marked as new resource
	if !newArt.IsNewResource {
		t.Errorf("New resource artifact should have IsNewResource=true")
	}

	// Should have patches that add spec
	if newArt.Patches == nil || len(newArt.Patches) == 0 {
		t.Errorf("New resource should have patches to add spec")
	}
}

func TestTransformResources_WhiteoutWithReplacement(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	o := &Orchestrator{
		Log:           logger,
		OptionalFlags: make(map[string]string),
	}

	buildConfig := &unstructured.Unstructured{}
	buildConfig.SetKind("BuildConfig")
	buildConfig.SetAPIVersion("build.openshift.io/v1")
	buildConfig.SetName("my-app")
	buildConfig.SetNamespace("default")

	mockPlugin := &mockNewResourcePlugin{
		name:         "BuildConfigPlugin",
		markWhiteout: true,
		newResources: []unstructured.Unstructured{
			{
				Object: map[string]interface{}{
					"apiVersion": "shipwright.io/v1beta1",
					"kind":       "Build",
					"metadata": map[string]interface{}{
						"name":      "my-app-build",
						"namespace": "default",
					},
				},
			},
		},
	}

	stage := Stage{
		DirName:    "10_BuildConfigPlugin",
		Priority:   10,
		PluginName: "BuildConfigPlugin",
	}

	artifacts, err := o.transformResources(stage, mockPlugin, []unstructured.Unstructured{*buildConfig})
	if err != nil {
		t.Fatalf("transformResources failed: %v", err)
	}

	if len(artifacts) != 2 {
		t.Fatalf("Expected 2 artifacts, got %d", len(artifacts))
	}

	// Original should be whiteout
	if !artifacts[0].HaveWhiteOut {
		t.Errorf("Original BuildConfig should be whiteout")
	}

	// New should NOT be whiteout
	if artifacts[1].HaveWhiteOut {
		t.Errorf("New Build should not be whiteout")
	}
}

func TestTransformResources_InvalidNewResource_MissingKind(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	o := &Orchestrator{
		Log:           logger,
		OptionalFlags: make(map[string]string),
	}

	input := &unstructured.Unstructured{}
	input.SetKind("Input")
	input.SetName("test-input")

	mockPlugin := &mockNewResourcePlugin{
		name: "InvalidPlugin",
		newResources: []unstructured.Unstructured{
			{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"metadata":   map[string]interface{}{"name": "invalid-resource"},
				},
			},
		},
	}

	stage := Stage{
		DirName:    "10_InvalidPlugin",
		PluginName: "InvalidPlugin",
	}

	_, err := o.transformResources(stage, mockPlugin, []unstructured.Unstructured{*input})
	if err == nil {
		t.Fatal("Expected error for missing kind")
	}
	if !strings.Contains(err.Error(), "missing kind") {
		t.Errorf("Error should mention 'missing kind', got: %v", err)
	}
}

func TestTransformResources_InvalidNewResource_MissingName(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	o := &Orchestrator{
		Log:           logger,
		OptionalFlags: make(map[string]string),
	}

	input := &unstructured.Unstructured{}
	input.SetKind("Input")
	input.SetName("test-input")

	mockPlugin := &mockNewResourcePlugin{
		name: "InvalidPlugin",
		newResources: []unstructured.Unstructured{
			{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]interface{}{},
				},
			},
		},
	}

	stage := Stage{
		DirName:    "10_InvalidPlugin",
		PluginName: "InvalidPlugin",
	}

	_, err := o.transformResources(stage, mockPlugin, []unstructured.Unstructured{*input})
	if err == nil {
		t.Fatal("Expected error for missing name")
	}
	if !strings.Contains(err.Error(), "missing name") {
		t.Errorf("Error should mention 'missing name', got: %v", err)
	}
}

func TestTransformResources_EmptyNewResources(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	o := &Orchestrator{
		Log:           logger,
		OptionalFlags: make(map[string]string),
	}

	configMap := &unstructured.Unstructured{}
	configMap.SetKind("ConfigMap")
	configMap.SetAPIVersion("v1")
	configMap.SetName("my-config")
	configMap.SetNamespace("default")

	mockPlugin := &mockNewResourcePlugin{
		name:         "OldPlugin",
		newResources: []unstructured.Unstructured{},
	}

	stage := Stage{
		DirName:    "10_OldPlugin",
		PluginName: "OldPlugin",
	}

	artifacts, err := o.transformResources(stage, mockPlugin, []unstructured.Unstructured{*configMap})
	if err != nil {
		t.Fatalf("transformResources failed: %v", err)
	}

	if len(artifacts) != 1 {
		t.Errorf("Expected 1 artifact (old plugin behavior), got %d", len(artifacts))
	}
}

func TestTransformResources_SkeletonHasMinimalFields(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	o := &Orchestrator{
		Log:           logger,
		OptionalFlags: make(map[string]string),
	}

	input := &unstructured.Unstructured{}
	input.SetKind("BuildConfig")
	input.SetAPIVersion("build.openshift.io/v1")
	input.SetName("my-app")
	input.SetNamespace("default")

	mockPlugin := &mockNewResourcePlugin{
		name: "TestPlugin",
		newResources: []unstructured.Unstructured{
			{
				Object: map[string]interface{}{
					"apiVersion": "shipwright.io/v1beta1",
					"kind":       "Build",
					"metadata": map[string]interface{}{
						"name":      "my-build",
						"namespace": "prod",
						"labels":    map[string]interface{}{"app": "test"},
					},
					"spec": map[string]interface{}{
						"output": map[string]interface{}{"image": "quay.io/test:latest"},
					},
				},
			},
		},
	}

	stage := Stage{DirName: "10_TestPlugin", PluginName: "TestPlugin"}

	artifacts, err := o.transformResources(stage, mockPlugin, []unstructured.Unstructured{*input})
	if err != nil {
		t.Fatalf("transformResources failed: %v", err)
	}

	newArt := artifacts[1]
	skel := newArt.Resource

	// Skeleton: only apiVersion, kind, metadata (name, namespace)
	if skel.GetAPIVersion() != "shipwright.io/v1beta1" {
		t.Errorf("skeleton apiVersion: got %q", skel.GetAPIVersion())
	}
	if skel.GetKind() != "Build" {
		t.Errorf("skeleton kind: got %q", skel.GetKind())
	}
	if skel.GetName() != "my-build" {
		t.Errorf("skeleton name: got %q", skel.GetName())
	}
	if skel.GetNamespace() != "prod" {
		t.Errorf("skeleton namespace: got %q", skel.GetNamespace())
	}

	// Labels should NOT be in skeleton (they're in the patch)
	skelMeta, _ := skel.Object["metadata"].(map[string]interface{})
	if _, hasLabels := skelMeta["labels"]; hasLabels {
		t.Errorf("skeleton should not have labels (should be in patch)")
	}

	// Spec should NOT be in skeleton
	if _, hasSpec := skel.Object["spec"]; hasSpec {
		t.Errorf("skeleton should not have spec")
	}

	// Patch should reconstruct the full resource
	skelJSON, _ := skel.MarshalJSON()
	patched, err := newArt.Patches.Apply(skelJSON)
	if err != nil {
		t.Fatalf("patch apply failed: %v", err)
	}
	var result unstructured.Unstructured
	result.UnmarshalJSON(patched)

	labels := result.GetLabels()
	if labels["app"] != "test" {
		t.Errorf("patched labels missing app=test, got %v", labels)
	}
	img, _, _ := unstructured.NestedString(result.Object, "spec", "output", "image")
	if img != "quay.io/test:latest" {
		t.Errorf("patched spec.output.image: got %q", img)
	}
}

// mockNewResourcePlugin is a mock plugin for testing NewResources feature
type mockNewResourcePlugin struct {
	name         string
	markWhiteout bool
	newResources []unstructured.Unstructured
}

func (m *mockNewResourcePlugin) Metadata() cranelib.PluginMetadata {
	return cranelib.PluginMetadata{
		Name:    m.name,
		Version: "v1",
	}
}

func (m *mockNewResourcePlugin) Run(request cranelib.PluginRequest) (cranelib.PluginResponse, error) {
	return cranelib.PluginResponse{
		Version:      "v1",
		IsWhiteOut:   m.markWhiteout,
		Patches:      jsonpatch.Patch{},
		NewResources: m.newResources,
	}, nil
}

func (m *mockNewResourcePlugin) GeneratePatches(resource unstructured.Unstructured) (jsonpatch.Patch, error) {
	return jsonpatch.Patch{}, nil
}
