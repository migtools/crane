package transform

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsDirectoryDirty(t *testing.T) {
	t.Run("clean directory with metadata", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create a file
		testFile := filepath.Join(tempDir, "test.txt")
		err := os.WriteFile(testFile, []byte("test content"), 0644)
		require.NoError(t, err)

		// Generate and write metadata
		hashes, err := GenerateContentHashes(tempDir)
		require.NoError(t, err)

		metadata := Metadata{
			CreatedAt:     time.Now(),
			CreatedBy:     "crane-transform",
			Plugin:        "test",
			CraneVersion:  "v1.0.0",
			ContentHashes: hashes,
		}

		err = WriteMetadata(tempDir, metadata)
		require.NoError(t, err)

		// Check if dirty
		dirty, err := IsDirectoryDirty(tempDir)
		require.NoError(t, err)
		assert.False(t, dirty, "directory should be clean")
	})

	t.Run("dirty directory - file modified", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create a file
		testFile := filepath.Join(tempDir, "test.txt")
		err := os.WriteFile(testFile, []byte("test content"), 0644)
		require.NoError(t, err)

		// Generate and write metadata
		hashes, err := GenerateContentHashes(tempDir)
		require.NoError(t, err)

		metadata := Metadata{
			CreatedAt:     time.Now(),
			CreatedBy:     "crane-transform",
			Plugin:        "test",
			CraneVersion:  "v1.0.0",
			ContentHashes: hashes,
		}

		err = WriteMetadata(tempDir, metadata)
		require.NoError(t, err)

		// Modify the file
		err = os.WriteFile(testFile, []byte("modified content"), 0644)
		require.NoError(t, err)

		// Check if dirty
		dirty, err := IsDirectoryDirty(tempDir)
		require.NoError(t, err)
		assert.True(t, dirty, "directory should be dirty after modification")
	})

	t.Run("dirty directory - file deleted", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create a file
		testFile := filepath.Join(tempDir, "test.txt")
		err := os.WriteFile(testFile, []byte("test content"), 0644)
		require.NoError(t, err)

		// Generate and write metadata
		hashes, err := GenerateContentHashes(tempDir)
		require.NoError(t, err)

		metadata := Metadata{
			CreatedAt:     time.Now(),
			CreatedBy:     "crane-transform",
			Plugin:        "test",
			CraneVersion:  "v1.0.0",
			ContentHashes: hashes,
		}

		err = WriteMetadata(tempDir, metadata)
		require.NoError(t, err)

		// Delete the file
		err = os.Remove(testFile)
		require.NoError(t, err)

		// Check if dirty
		dirty, err := IsDirectoryDirty(tempDir)
		require.NoError(t, err)
		assert.True(t, dirty, "directory should be dirty after file deletion")
	})

	t.Run("dirty directory - new file added", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create initial file
		testFile := filepath.Join(tempDir, "test.txt")
		err := os.WriteFile(testFile, []byte("test content"), 0644)
		require.NoError(t, err)

		// Generate and write metadata
		hashes, err := GenerateContentHashes(tempDir)
		require.NoError(t, err)

		metadata := Metadata{
			CreatedAt:     time.Now(),
			CreatedBy:     "crane-transform",
			Plugin:        "test",
			CraneVersion:  "v1.0.0",
			ContentHashes: hashes,
		}

		err = WriteMetadata(tempDir, metadata)
		require.NoError(t, err)

		// Add a new file
		newFile := filepath.Join(tempDir, "new.txt")
		err = os.WriteFile(newFile, []byte("new content"), 0644)
		require.NoError(t, err)

		// Check if dirty
		dirty, err := IsDirectoryDirty(tempDir)
		require.NoError(t, err)
		assert.True(t, dirty, "directory should be dirty after adding new file")
	})

	t.Run("no metadata - treat as clean", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create a file but no metadata
		testFile := filepath.Join(tempDir, "test.txt")
		err := os.WriteFile(testFile, []byte("test content"), 0644)
		require.NoError(t, err)

		// Check if dirty
		dirty, err := IsDirectoryDirty(tempDir)
		require.NoError(t, err)
		assert.False(t, dirty, "directory without metadata should be clean")
	})
}

func TestEnsureCleanDirectory(t *testing.T) {
	t.Run("directory doesn't exist", func(t *testing.T) {
		tempDir := filepath.Join(t.TempDir(), "nonexistent")

		err := EnsureCleanDirectory(tempDir, false)
		assert.NoError(t, err, "non-existent directory should pass")
	})

	t.Run("clean directory without force", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create metadata for clean directory
		hashes, err := GenerateContentHashes(tempDir)
		require.NoError(t, err)

		metadata := Metadata{
			CreatedAt:     time.Now(),
			CreatedBy:     "crane-transform",
			Plugin:        "test",
			CraneVersion:  "v1.0.0",
			ContentHashes: hashes,
		}

		err = WriteMetadata(tempDir, metadata)
		require.NoError(t, err)

		err = EnsureCleanDirectory(tempDir, false)
		assert.NoError(t, err, "clean directory should pass")
	})

	t.Run("dirty directory without force - fails", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create a file
		testFile := filepath.Join(tempDir, "test.txt")
		err := os.WriteFile(testFile, []byte("test content"), 0644)
		require.NoError(t, err)

		// Generate metadata
		hashes, err := GenerateContentHashes(tempDir)
		require.NoError(t, err)

		metadata := Metadata{
			CreatedAt:     time.Now(),
			CreatedBy:     "crane-transform",
			Plugin:        "test",
			CraneVersion:  "v1.0.0",
			ContentHashes: hashes,
		}

		err = WriteMetadata(tempDir, metadata)
		require.NoError(t, err)

		// Modify the file to make directory dirty
		err = os.WriteFile(testFile, []byte("modified"), 0644)
		require.NoError(t, err)

		// Should fail without force
		err = EnsureCleanDirectory(tempDir, false)
		assert.Error(t, err, "dirty directory should fail without force")
		assert.Contains(t, err.Error(), "contains user modifications")
	})

	t.Run("dirty directory with force - passes", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create a file
		testFile := filepath.Join(tempDir, "test.txt")
		err := os.WriteFile(testFile, []byte("test content"), 0644)
		require.NoError(t, err)

		// Generate metadata
		hashes, err := GenerateContentHashes(tempDir)
		require.NoError(t, err)

		metadata := Metadata{
			CreatedAt:     time.Now(),
			CreatedBy:     "crane-transform",
			Plugin:        "test",
			CraneVersion:  "v1.0.0",
			ContentHashes: hashes,
		}

		err = WriteMetadata(tempDir, metadata)
		require.NoError(t, err)

		// Modify the file to make directory dirty
		err = os.WriteFile(testFile, []byte("modified"), 0644)
		require.NoError(t, err)

		// Should pass with force
		err = EnsureCleanDirectory(tempDir, true)
		assert.NoError(t, err, "dirty directory should pass with force flag")
	})
}

func TestGenerateContentHashes(t *testing.T) {
	tempDir := t.TempDir()

	// Create test files
	file1 := filepath.Join(tempDir, "file1.txt")
	file2 := filepath.Join(tempDir, "file2.txt")

	err := os.WriteFile(file1, []byte("content1"), 0644)
	require.NoError(t, err)

	err = os.WriteFile(file2, []byte("content2"), 0644)
	require.NoError(t, err)

	// Generate hashes
	hashes, err := GenerateContentHashes(tempDir)
	require.NoError(t, err)

	// Verify both files are hashed
	assert.Contains(t, hashes, "file1.txt")
	assert.Contains(t, hashes, "file2.txt")

	// Verify hash format
	assert.Contains(t, hashes["file1.txt"], "sha256:")
	assert.Contains(t, hashes["file2.txt"], "sha256:")

	// Verify hashes are different for different content
	assert.NotEqual(t, hashes["file1.txt"], hashes["file2.txt"])
}
