package transform

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Metadata represents stage directory metadata for dirty check
type Metadata struct {
	CreatedAt      time.Time         `json:"createdAt"`
	CreatedBy      string            `json:"createdBy"`
	Plugin         string            `json:"plugin"`
	PluginVersion  string            `json:"pluginVersion,omitempty"`
	CraneVersion   string            `json:"craneVersion"`
	ContentHashes  map[string]string `json:"contentHashes"`
}

// WriteMetadata writes metadata file to stage directory
func WriteMetadata(stageDir string, metadata Metadata) error {
	metadataPath := filepath.Join(stageDir, ".crane-metadata.json")

	jsonBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, jsonBytes, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// ReadMetadata reads metadata file from stage directory
func ReadMetadata(stageDir string) (Metadata, error) {
	metadataPath := filepath.Join(stageDir, ".crane-metadata.json")

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return Metadata{}, fmt.Errorf("failed to read metadata file: %w", err)
	}

	var metadata Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return Metadata{}, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return metadata, nil
}

// GenerateContentHashes generates SHA256 hashes for all files in stage directory
// Excludes .crane-metadata.json itself
func GenerateContentHashes(stageDir string) (map[string]string, error) {
	hashes := make(map[string]string)

	// Walk through stage directory
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

		// Compute hash
		hash, err := computeSHA256(path)
		if err != nil {
			return fmt.Errorf("failed to hash %s: %w", relPath, err)
		}

		hashes[relPath] = hash
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to generate content hashes: %w", err)
	}

	return hashes, nil
}

// computeSHA256 computes SHA256 hash of a file
func computeSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", hash), nil
}
