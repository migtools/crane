package transform

import (
	"os"
	"path/filepath"
	"testing"

	jsonpatch "github.com/evanphx/json-patch"
	cranelib "github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane/internal/file"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestWriteStageWithNonExistentRemovePath(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "crane-writer-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	transformDir := filepath.Join(tmpDir, "transform")
	stageName := "10_test"

	// Create a Service resource WITHOUT externalIPs field
	resource := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]interface{}{
				"name":            "test-service",
				"namespace":       "default",
				"uid":             "abc-123",
				"resourceVersion": "12345",
			},
			"spec": map[string]interface{}{
				"type": "ClusterIP",
				"ports": []interface{}{
					map[string]interface{}{
						"port":       80,
						"targetPort": 8080,
					},
				},
			},
			"status": map[string]interface{}{
				"loadBalancer": map[string]interface{}{},
			},
		},
	}

	// Create patches including a remove for non-existent /spec/externalIPs
	// This simulates the error: "Unable to remove nonexistent key: externalIPs"
	patchJSON := `[
		{"op": "remove", "path": "/metadata/uid"},
		{"op": "remove", "path": "/metadata/resourceVersion"},
		{"op": "remove", "path": "/spec/externalIPs"},
		{"op": "remove", "path": "/status"}
	]`

	patches, err := jsonpatch.DecodePatch([]byte(patchJSON))
	if err != nil {
		t.Fatalf("Failed to decode patches: %v", err)
	}

	// Create artifact
	artifact := cranelib.TransformArtifact{
		Resource:     resource,
		HaveWhiteOut: false,
		Patches:      patches,
		Target:       cranelib.DeriveTargetFromResource(resource),
		PluginName:   "test-plugin",
	}

	// Create writer
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	opts := file.PathOpts{
		TransformDir: transformDir,
	}
	writer := NewKustomizeWriter(opts, stageName, logger)

	// Write stage - this should NOT fail even though /spec/externalIPs doesn't exist
	err = writer.WriteStage([]cranelib.TransformArtifact{artifact}, false)
	if err != nil {
		t.Fatalf("WriteStage failed: %v", err)
	}

	// Verify patch file was created and doesn't include the non-existent path
	patchPath := filepath.Join(transformDir, stageName, "patches", "default--v1--Service--test-service.patch.yaml")
	patchData, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatalf("Failed to read patch file: %v", err)
	}

	// The patch should NOT contain /spec/externalIPs
	patchStr := string(patchData)
	if contains(patchStr, "/spec/externalIPs") {
		t.Errorf("Patch file contains /spec/externalIPs which doesn't exist in resource:\n%s", patchStr)
	}

	// The patch SHOULD contain the other remove operations
	if !contains(patchStr, "/metadata/uid") {
		t.Errorf("Patch file missing /metadata/uid:\n%s", patchStr)
	}
	if !contains(patchStr, "/metadata/resourceVersion") {
		t.Errorf("Patch file missing /metadata/resourceVersion:\n%s", patchStr)
	}
	if !contains(patchStr, "/status") {
		t.Errorf("Patch file missing /status:\n%s", patchStr)
	}

	// Verify kustomization.yaml exists
	kustomizationPath := filepath.Join(transformDir, stageName, "kustomization.yaml")
	if _, err := os.Stat(kustomizationPath); os.IsNotExist(err) {
		t.Errorf("kustomization.yaml not created")
	}
}
