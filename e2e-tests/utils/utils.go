package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CreateTempDir creates a temporary directory with the given prefix.
func CreateTempDir(prefix string) (string, error) {
	return os.MkdirTemp("", prefix)
}

// ListFilesRecursively returns a formatted list of files under a directory.
func ListFilesRecursively(dir string) (string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Strings(files)
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

// HasFilesRecursively reports whether a directory contains any files.
func HasFilesRecursively(dir string) (bool, string, error) {
	files, err := ListFilesRecursively(dir)
	if err != nil {
		return false, "", err
	}
	hasFiles := !strings.Contains(files, "(no files)")
	return hasFiles, files, nil
}
