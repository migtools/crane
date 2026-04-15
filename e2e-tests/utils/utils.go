package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// CreateTempDir creates a temporary directory with the given prefix.
func CreateTempDir(prefix string) (string, error) {
	return os.MkdirTemp("", prefix)
}

// ListFilesRecursively returns a formatted list of files under a directory.
func ListFilesRecursively(dir string) (string, error) {
	files, err := ListFilesRecursivelyAsList(dir)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "  (no files)", nil
	}

	var b strings.Builder
	for _, path := range files {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			rel = path
		}
		b.WriteString(fmt.Sprintf("  - %s\n", rel))
	}

	return strings.TrimRight(b.String(), "\n"), nil
}

// ListFilesRecursivelyAsList returns sorted file paths under dir as relative paths.
func ListFilesRecursivelyAsList(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			rel = path
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// HasFilesRecursively reports whether a directory contains any files.
func HasFilesRecursively(dir string) (bool, string, error) {
	files, err := ListFilesRecursively(dir)
	if err != nil {
		return false, "", err
	}
	hasFiles := !strings.Contains(files, "(no files)")
	return hasFiles, files, nil
}

// ReadTestdataFile reads a file from the testdata directory.
func ReadTestdataFile(filename string) (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}

	baseDir := filepath.Dir(thisFile)
	path := filepath.Join(baseDir, "..", "testdata", filename)

	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}

	return string(b), nil
}

// LooksLikeYAMLFile returns true for paths that look like YAML (by extension or no extension, e.g. output fragments).
func LooksLikeYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		return true
	default:
		return ext == ""
	}
}
