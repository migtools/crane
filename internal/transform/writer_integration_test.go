package transform

import (
	"os"
	"path/filepath"
	"strings"
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

func TestWriteStage_ClusterScopedResources(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crane-writer-cluster-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	transformDir := filepath.Join(tmpDir, "transform")
	stageName := "10_test"

	clusterRole := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata": map[string]interface{}{
				"name": "test-reader",
			},
			"rules": []interface{}{
				map[string]interface{}{
					"apiGroups": []interface{}{""},
					"resources": []interface{}{"pods"},
					"verbs":     []interface{}{"get", "list"},
				},
			},
		},
	}

	clusterRoleBinding := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRoleBinding",
			"metadata": map[string]interface{}{
				"name": "test-reader-binding",
			},
			"roleRef": map[string]interface{}{
				"apiGroup": "rbac.authorization.k8s.io",
				"kind":     "ClusterRole",
				"name":     "test-reader",
			},
			"subjects": []interface{}{
				map[string]interface{}{
					"kind":      "ServiceAccount",
					"name":      "test-sa",
					"namespace": "my-app",
				},
			},
		},
	}

	artifacts := []cranelib.TransformArtifact{
		{
			Resource:     clusterRole,
			HaveWhiteOut: false,
			Target:       cranelib.DeriveTargetFromResource(clusterRole),
			PluginName:   "test-plugin",
		},
		{
			Resource:     clusterRoleBinding,
			HaveWhiteOut: false,
			Target:       cranelib.DeriveTargetFromResource(clusterRoleBinding),
			PluginName:   "test-plugin",
		},
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	opts := file.PathOpts{TransformDir: transformDir}
	writer := NewKustomizeWriter(opts, stageName, logger)

	if err := writer.WriteStage(artifacts, false); err != nil {
		t.Fatalf("WriteStage failed: %v", err)
	}

	// Verify resource files use "clusterscoped" in filename
	resourcesDir := filepath.Join(transformDir, stageName, "resources")
	entries, err := os.ReadDir(resourcesDir)
	if err != nil {
		t.Fatalf("Failed to read resources dir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Expected 2 resource files, got %d", len(entries))
	}
	for _, entry := range entries {
		if !strings.Contains(entry.Name(), "clusterscoped") {
			t.Errorf("Expected 'clusterscoped' in filename, got: %s", entry.Name())
		}
	}

	// Verify kustomization.yaml lists resources and patch targets omit namespace
	kustomizationPath := filepath.Join(transformDir, stageName, "kustomization.yaml")
	kData, err := os.ReadFile(kustomizationPath)
	if err != nil {
		t.Fatalf("Failed to read kustomization.yaml: %v", err)
	}
	kStr := string(kData)

	if !strings.Contains(kStr, "ClusterRole_rbac.authorization.k8s.io_v1_clusterscoped_test-reader.yaml") {
		t.Error("kustomization.yaml missing ClusterRole resource reference")
	}
	if !strings.Contains(kStr, "ClusterRoleBinding_rbac.authorization.k8s.io_v1_clusterscoped_test-reader-binding.yaml") {
		t.Error("kustomization.yaml missing ClusterRoleBinding resource reference")
	}
	// Cluster-scoped patch targets must not have namespace
	if strings.Contains(kStr, "namespace:") {
		t.Error("kustomization.yaml should not contain 'namespace:' for cluster-scoped resources")
	}
}

func TestWriteStage_MixedNamespacedAndClusterScoped(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crane-writer-mixed-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	transformDir := filepath.Join(tmpDir, "transform")
	stageName := "10_test"

	deployment := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "web",
				"namespace": "my-app",
			},
			"spec": map[string]interface{}{
				"replicas": int64(1),
			},
		},
	}

	clusterRole := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata": map[string]interface{}{
				"name": "web-admin",
			},
			"rules": []interface{}{
				map[string]interface{}{
					"apiGroups": []interface{}{""},
					"resources": []interface{}{"pods"},
					"verbs":     []interface{}{"get"},
				},
			},
		},
	}

	patchJSON := `[{"op": "remove", "path": "/spec/replicas"}]`
	deployPatches, err := jsonpatch.DecodePatch([]byte(patchJSON))
	if err != nil {
		t.Fatalf("Failed to decode deployment patch JSON: %v", err)
	}

	clusterPatchJSON := `[{"op": "add", "path": "/metadata/labels", "value": {"migrated": "true"}}]`
	clusterPatches, err := jsonpatch.DecodePatch([]byte(clusterPatchJSON))
	if err != nil {
		t.Fatalf("Failed to decode ClusterRole patch JSON: %v", err)
	}

	artifacts := []cranelib.TransformArtifact{
		{
			Resource:     deployment,
			HaveWhiteOut: false,
			Patches:      deployPatches,
			Target:       cranelib.DeriveTargetFromResource(deployment),
			PluginName:   "test-plugin",
		},
		{
			Resource:     clusterRole,
			HaveWhiteOut: false,
			Patches:      clusterPatches,
			Target:       cranelib.DeriveTargetFromResource(clusterRole),
			PluginName:   "test-plugin",
		},
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	opts := file.PathOpts{TransformDir: transformDir}
	writer := NewKustomizeWriter(opts, stageName, logger)

	if err := writer.WriteStage(artifacts, false); err != nil {
		t.Fatalf("WriteStage failed: %v", err)
	}

	// Verify both resource files exist
	resourcesDir := filepath.Join(transformDir, stageName, "resources")
	entries, err := os.ReadDir(resourcesDir)
	if err != nil {
		t.Fatalf("Failed to read resources dir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Expected 2 resource files, got %d", len(entries))
	}

	// Verify patch files: namespaced has namespace prefix, cluster-scoped does not
	patchesDir := filepath.Join(transformDir, stageName, "patches")
	patchEntries, err := os.ReadDir(patchesDir)
	if err != nil {
		t.Fatalf("Failed to read patches dir: %v", err)
	}
	if len(patchEntries) != 2 {
		t.Fatalf("Expected 2 patch files, got %d", len(patchEntries))
	}

	foundNamespacedPatch := false
	foundClusterPatch := false
	for _, entry := range patchEntries {
		if strings.HasPrefix(entry.Name(), "my-app--") {
			foundNamespacedPatch = true
		}
		if strings.HasPrefix(entry.Name(), "rbac.authorization.k8s.io-v1--ClusterRole") {
			foundClusterPatch = true
		}
	}
	if !foundNamespacedPatch {
		t.Error("Missing namespaced patch file with namespace prefix")
	}
	if !foundClusterPatch {
		t.Error("Missing cluster-scoped patch file without namespace prefix")
	}

	// Verify kustomization.yaml has both namespaced and cluster-scoped targets
	kData, err := os.ReadFile(filepath.Join(transformDir, stageName, "kustomization.yaml"))
	if err != nil {
		t.Fatalf("Failed to read kustomization.yaml: %v", err)
	}
	kStr := string(kData)

	if !strings.Contains(kStr, "namespace: my-app") {
		t.Error("kustomization.yaml missing 'namespace: my-app' for namespaced Deployment")
	}

	// Check that ClusterRole target does not have namespace
	// The ClusterRole section should have kind+name but no namespace
	lines := strings.Split(kStr, "\n")
	inClusterRoleTarget := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "kind: ClusterRole" {
			inClusterRoleTarget = true
		}
		if inClusterRoleTarget && strings.HasPrefix(trimmed, "namespace:") {
			t.Error("ClusterRole patch target should not have 'namespace:' field")
			break
		}
		if inClusterRoleTarget && trimmed == "" {
			break
		}
	}
}

func TestWriteStage_ClusterScopedWhiteout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crane-writer-whiteout-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	transformDir := filepath.Join(tmpDir, "transform")
	stageName := "10_test"

	clusterRole := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata": map[string]interface{}{
				"name": "whiteout-role",
			},
			"rules": []interface{}{},
		},
	}

	artifacts := []cranelib.TransformArtifact{
		{
			Resource:     clusterRole,
			HaveWhiteOut: true,
			Target:       cranelib.DeriveTargetFromResource(clusterRole),
			PluginName:   "test-plugin",
		},
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	opts := file.PathOpts{TransformDir: transformDir}
	writer := NewKustomizeWriter(opts, stageName, logger)

	if err := writer.WriteStage(artifacts, false); err != nil {
		t.Fatalf("WriteStage failed: %v", err)
	}

	// Verify resource file is written to disk
	resourceFile := filepath.Join(transformDir, stageName, "resources",
		"ClusterRole_rbac.authorization.k8s.io_v1_clusterscoped_whiteout-role.yaml")
	if _, err := os.Stat(resourceFile); os.IsNotExist(err) {
		t.Error("Whiteout cluster-scoped resource should still be written to disk")
	}

	// Verify it's commented out in kustomization.yaml
	kData, err := os.ReadFile(filepath.Join(transformDir, stageName, "kustomization.yaml"))
	if err != nil {
		t.Fatalf("Failed to read kustomization.yaml: %v", err)
	}
	kStr := string(kData)

	// Check the resource is NOT active (uncommented) but IS commented out
	// We need to be precise: "# - resources/..." (commented) also contains "- resources/..." as substring
	resourceRef := "resources/ClusterRole_rbac.authorization.k8s.io_v1_clusterscoped_whiteout-role.yaml"
	lines := strings.Split(kStr, "\n")
	foundActive := false
	foundCommented := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "- "+resourceRef {
			foundActive = true
		}
		if trimmed == "# - "+resourceRef {
			foundCommented = true
		}
	}
	if foundActive {
		t.Error("Whiteout resource should NOT be in active resources list")
	}
	if !foundCommented {
		t.Error("Whiteout resource should be in commented-out section")
	}
}

func TestWriteStage_ClusterScopedWithPatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crane-writer-cluster-patch-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	transformDir := filepath.Join(tmpDir, "transform")
	stageName := "10_test"

	clusterRole := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata": map[string]interface{}{
				"name":            "patched-role",
				"uid":             "should-be-removed",
				"resourceVersion": "12345",
			},
			"rules": []interface{}{
				map[string]interface{}{
					"apiGroups": []interface{}{""},
					"resources": []interface{}{"pods"},
					"verbs":     []interface{}{"get"},
				},
			},
		},
	}

	patchJSON := `[
		{"op": "remove", "path": "/metadata/uid"},
		{"op": "remove", "path": "/metadata/resourceVersion"}
	]`
	patches, err := jsonpatch.DecodePatch([]byte(patchJSON))
	if err != nil {
		t.Fatalf("Failed to decode ClusterRole patch JSON: %v", err)
	}

	artifacts := []cranelib.TransformArtifact{
		{
			Resource:     clusterRole,
			HaveWhiteOut: false,
			Patches:      patches,
			Target:       cranelib.DeriveTargetFromResource(clusterRole),
			PluginName:   "test-plugin",
		},
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	opts := file.PathOpts{TransformDir: transformDir}
	writer := NewKustomizeWriter(opts, stageName, logger)

	if err := writer.WriteStage(artifacts, false); err != nil {
		t.Fatalf("WriteStage failed: %v", err)
	}

	// Verify patch file created with correct name (no namespace prefix)
	expectedPatchFile := "rbac.authorization.k8s.io-v1--ClusterRole--patched-role.patch.yaml"
	patchPath := filepath.Join(transformDir, stageName, "patches", expectedPatchFile)
	if _, err := os.Stat(patchPath); os.IsNotExist(err) {
		t.Errorf("Expected patch file %s not found", expectedPatchFile)
	}

	// Verify patch content
	patchData, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatalf("Failed to read patch file: %v", err)
	}
	if !strings.Contains(string(patchData), "/metadata/uid") {
		t.Error("Patch file should contain remove for /metadata/uid")
	}

	// Verify kustomization.yaml patch target omits namespace
	kData, err := os.ReadFile(filepath.Join(transformDir, stageName, "kustomization.yaml"))
	if err != nil {
		t.Fatalf("Failed to read kustomization.yaml: %v", err)
	}
	kStr := string(kData)

	if !strings.Contains(kStr, "kind: ClusterRole") {
		t.Error("kustomization.yaml missing ClusterRole in patch target")
	}
	if !strings.Contains(kStr, "name: patched-role") {
		t.Error("kustomization.yaml missing name in patch target")
	}
	if strings.Contains(kStr, "namespace:") {
		t.Error("kustomization.yaml should not contain namespace for cluster-scoped patch target")
	}
}

func TestWriteStage_KustomizeBuildWithMixedResources(t *testing.T) {
	if !hasKustomizeCommand(t) {
		t.Skip("kubectl/oc not available, skipping kustomize build test")
	}

	tmpDir, err := os.MkdirTemp("", "crane-writer-kustomize-mixed-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	transformDir := filepath.Join(tmpDir, "transform")
	stageName := "10_test"

	deployment := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "web",
				"namespace": "my-app",
			},
			"spec": map[string]interface{}{
				"replicas": int64(1),
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{"app": "web"},
				},
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{"app": "web"},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "web",
								"image": "nginx:1.21",
							},
						},
					},
				},
			},
		},
	}

	clusterRole := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata": map[string]interface{}{
				"name": "web-reader",
			},
			"rules": []interface{}{
				map[string]interface{}{
					"apiGroups": []interface{}{""},
					"resources": []interface{}{"pods"},
					"verbs":     []interface{}{"get", "list"},
				},
			},
		},
	}

	artifacts := []cranelib.TransformArtifact{
		{
			Resource:     deployment,
			HaveWhiteOut: false,
			Target:       cranelib.DeriveTargetFromResource(deployment),
			PluginName:   "test-plugin",
		},
		{
			Resource:     clusterRole,
			HaveWhiteOut: false,
			Target:       cranelib.DeriveTargetFromResource(clusterRole),
			PluginName:   "test-plugin",
		},
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	opts := file.PathOpts{TransformDir: transformDir}
	writer := NewKustomizeWriter(opts, stageName, logger)

	if err := writer.WriteStage(artifacts, false); err != nil {
		t.Fatalf("WriteStage failed: %v", err)
	}

	// Run kubectl kustomize to verify the output is valid
	stageDir := filepath.Join(transformDir, stageName)
	o := &Orchestrator{
		Log:       logger,
		ExportDir: tmpDir,
	}
	resources, err := o.applyStageTransforms(stageDir)
	if err != nil {
		t.Fatalf("kubectl kustomize failed on mixed resources: %v", err)
	}

	if len(resources) != 2 {
		t.Fatalf("Expected 2 resources from kustomize build, got %d", len(resources))
	}

	foundDeployment := false
	foundClusterRole := false
	for _, r := range resources {
		switch r.GetKind() {
		case "Deployment":
			foundDeployment = true
			if r.GetNamespace() != "my-app" {
				t.Errorf("Deployment should have namespace 'my-app', got '%s'", r.GetNamespace())
			}
		case "ClusterRole":
			foundClusterRole = true
			if r.GetNamespace() != "" {
				t.Errorf("ClusterRole should have empty namespace, got '%s'", r.GetNamespace())
			}
		}
	}

	if !foundDeployment {
		t.Error("Deployment not found in kustomize output")
	}
	if !foundClusterRole {
		t.Error("ClusterRole not found in kustomize output")
	}
}
