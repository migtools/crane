package transform

import (
	"fmt"
	"os"
	"path/filepath"
)

// IsDirectoryDirty checks if a stage directory has been modified since creation
// Returns true if directory is dirty (modified), false if clean
func IsDirectoryDirty(stageDir string) (bool, error) {
	metadataPath := filepath.Join(stageDir, ".crane-metadata.json")

	// If metadata doesn't exist, treat as clean (first run)
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return false, nil
	}

	// Read metadata
	metadata, err := ReadMetadata(stageDir)
	if err != nil {
		return false, fmt.Errorf("failed to read metadata: %w", err)
	}

	// Check each file hash
	for relPath, expectedHash := range metadata.ContentHashes {
		fullPath := filepath.Join(stageDir, relPath)

		// Check if file exists
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			// File was deleted - directory is dirty
			return true, nil
		}

		// Compute current hash
		currentHash, err := computeSHA256(fullPath)
		if err != nil {
			return false, fmt.Errorf("failed to compute hash for %s: %w", relPath, err)
		}

		// Compare hashes
		if currentHash != expectedHash {
			// Hash mismatch - directory is dirty
			return true, nil
		}
	}

	// Check for new files not in metadata
	actualFiles, err := listAllFiles(stageDir)
	if err != nil {
		return false, fmt.Errorf("failed to list files: %w", err)
	}

	// Check if there are files not in metadata
	for _, file := range actualFiles {
		if _, exists := metadata.ContentHashes[file]; !exists {
			// New file found - directory is dirty
			return true, nil
		}
	}

	// All checks passed - directory is clean
	return false, nil
}

// listAllFiles returns all files in directory (excluding .crane-metadata.json)
func listAllFiles(stageDir string) ([]string, error) {
	var files []string

	err := filepath.Walk(stageDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(stageDir, path)
		if err != nil {
			return err
		}

		// Skip metadata file itself
		if relPath == ".crane-metadata.json" {
			return nil
		}

		files = append(files, relPath)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

// EnsureCleanDirectory checks if directory is clean or doesn't exist
// Returns error if directory exists and is dirty
func EnsureCleanDirectory(stageDir string, force bool) error {
	// Check if directory exists
	if _, err := os.Stat(stageDir); os.IsNotExist(err) {
		// Directory doesn't exist - clean
		return nil
	}

	// If force flag is set, allow overwrite
	if force {
		return nil
	}

	// Check if directory is dirty
	dirty, err := IsDirectoryDirty(stageDir)
	if err != nil {
		return fmt.Errorf("failed to check directory status: %w", err)
	}

	if dirty {
		return fmt.Errorf("stage directory '%s' contains user modifications - use --force to overwrite or remove/rename the directory", filepath.Base(stageDir))
	}

	// Directory exists and is clean - allow overwrite
	return nil
}
