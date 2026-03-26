// +build integration

package transform_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	cranelib "github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane/internal/file"
	"github.com/konveyor/crane/internal/transform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKustomizeWriterIntegration(t *testing.T) {
	// Setup test environment
	tempDir := t.TempDir()
	exportDir := filepath.Join(tempDir, "export")
	transformDir := filepath.Join(tempDir, "transform")

	// Copy test fixtures to export directory
	fixturesDir := filepath.Join("..", "..", "test-data", "kustomize-transform", "fixtures")
	err := copyDir(fixturesDir, exportDir)
	require.NoError(t, err)

	// Read resources from export directory
	files, err := file.ReadFiles(context.TODO(), exportDir)
	require.NoError(t, err)
	require.NotEmpty(t, files, "should have resources from fixtures")

	// Create transform artifacts
	var artifacts []cranelib.TransformArtifact
	for _, f := range files {
		artifact := cranelib.TransformArtifact{
			Resource:     f.Unstructured,
			HaveWhiteOut: false,
			Patches:      nil, // No patches for this test
			IgnoredOps:   []cranelib.IgnoredOperation{},
			Target:       cranelib.DeriveTargetFromResource(f.Unstructured),
			PluginName:   "test-plugin",
		}
		artifacts = append(artifacts, artifact)
	}

	// Write stage using KustomizeWriter
	opts := file.PathOpts{
		TransformDir: transformDir,
		ExportDir:    exportDir,
	}

	writer := transform.NewKustomizeWriter(opts, "10_test", "test-plugin", "v1.0.0", "v1.0.0")
	err = writer.WriteStage(artifacts, false)
	require.NoError(t, err)

	// Verify stage directory structure
	stageDir := opts.GetStageDir("10_test")
	assert.DirExists(t, stageDir)

	resourcesDir := opts.GetResourcesDir("10_test")
	assert.DirExists(t, resourcesDir)

	patchesDir := opts.GetPatchesDir("10_test")
	assert.DirExists(t, patchesDir)

	kustomizationPath := opts.GetKustomizationPath("10_test")
	assert.FileExists(t, kustomizationPath)

	metadataPath := opts.GetMetadataPath("10_test")
	assert.FileExists(t, metadataPath)

	// Verify resources were written
	entries, err := os.ReadDir(resourcesDir)
	require.NoError(t, err)
	assert.NotEmpty(t, entries, "resources directory should contain files")

	// Verify metadata
	metadata, err := transform.ReadMetadata(stageDir)
	require.NoError(t, err)
	assert.Equal(t, "test-plugin", metadata.Plugin)
	assert.Equal(t, "v1.0.0", metadata.CraneVersion)
	assert.NotEmpty(t, metadata.ContentHashes)

	// Test dirty check - directory should be clean
	dirty, err := transform.IsDirectoryDirty(stageDir)
	require.NoError(t, err)
	assert.False(t, dirty, "newly written stage should be clean")
}

func TestStageDiscoveryIntegration(t *testing.T) {
	tempDir := t.TempDir()

	// Create multiple stages
	stageNames := []string{"10_kubernetes", "20_openshift", "30_imagestream"}
	for _, stageName := range stageNames {
		stageDir := filepath.Join(tempDir, stageName)
		err := os.Mkdir(stageDir, 0755)
		require.NoError(t, err)
	}

	// Discover stages
	stages, err := transform.DiscoverStages(tempDir)
	require.NoError(t, err)
	require.Len(t, stages, 3)

	// Verify ordering
	assert.Equal(t, 10, stages[0].Priority)
	assert.Equal(t, "kubernetes", stages[0].PluginName)

	assert.Equal(t, 20, stages[1].Priority)
	assert.Equal(t, "openshift", stages[1].PluginName)

	assert.Equal(t, 30, stages[2].Priority)
	assert.Equal(t, "imagestream", stages[2].PluginName)

	// Test filtering
	selector := transform.StageSelector{
		FromStage: "20_openshift",
	}
	filtered := transform.FilterStages(stages, selector)
	require.Len(t, filtered, 2)
	assert.Equal(t, "openshift", filtered[0].PluginName)
	assert.Equal(t, "imagestream", filtered[1].PluginName)
}

func TestDirtyCheckIntegration(t *testing.T) {
	tempDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tempDir, "test.txt")
	err := os.WriteFile(testFile, []byte("original content"), 0644)
	require.NoError(t, err)

	// Generate and write metadata
	hashes, err := transform.GenerateContentHashes(tempDir)
	require.NoError(t, err)

	metadata := transform.Metadata{
		CreatedBy:     "crane-transform",
		Plugin:        "test",
		CraneVersion:  "v1.0.0",
		ContentHashes: hashes,
	}

	err = transform.WriteMetadata(tempDir, metadata)
	require.NoError(t, err)

	// Directory should be clean
	dirty, err := transform.IsDirectoryDirty(tempDir)
	require.NoError(t, err)
	assert.False(t, dirty)

	// Modify the file
	err = os.WriteFile(testFile, []byte("modified content"), 0644)
	require.NoError(t, err)

	// Directory should now be dirty
	dirty, err = transform.IsDirectoryDirty(tempDir)
	require.NoError(t, err)
	assert.True(t, dirty)

	// EnsureCleanDirectory should fail without force
	err = transform.EnsureCleanDirectory(tempDir, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user modifications")

	// EnsureCleanDirectory should succeed with force
	err = transform.EnsureCleanDirectory(tempDir, true)
	assert.NoError(t, err)
}

// Helper function to copy directory
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(dstPath, data, info.Mode())
	})
}
