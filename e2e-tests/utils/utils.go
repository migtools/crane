package utils

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
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

// GoldenManifestsDir returns the path to the golden fixtures directory for an app and pipeline stage.
// It resolves e2e-tests/golden-manifests/<appName>/<stage> relative to this file's location, so the
// result does not depend on the process working directory. Valid stage values are "export", "transform",
// and "output". appName must be non-empty.
//
// The returned path is cleaned by filepath.Join (and is absolute when the runtime reports an absolute
// path for this source file). The directory is not required to exist; callers should stat or list it if needed.
func GoldenManifestsDir(appName, stage string) (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}

	if appName == "" {
		return "", fmt.Errorf("appName is required")
	}
	if stage != "export" && stage != "transform" && stage != "output" {
		return "", fmt.Errorf("invalid stage: %s", stage)
	}

	baseDir := filepath.Dir(thisFile)
	path := filepath.Join(baseDir, "..", "golden-manifests", appName, stage)
	return path, nil
}

// CompareDirectoryFileSets compares the file sets in two directories and returns an error if they differ.
func CompareDirectoryFileSets(goldenDir, gotDir string) error {
	goldenInfo, err := os.Stat(goldenDir)
	if err != nil {
		return fmt.Errorf("golden directory %q: %w", goldenDir, err)
	}
	if !goldenInfo.IsDir() {
		return fmt.Errorf("golden path %q is not a directory", goldenDir)
	}

	gotInfo, err := os.Stat(gotDir)
	if err != nil {
		return fmt.Errorf("got directory %q: %w", gotDir, err)
	}
	if !gotInfo.IsDir() {
		return fmt.Errorf("got path %q is not a directory", gotDir)
	}

	goldenFiles, err := ListFilesRecursivelyAsList(goldenDir)
	if err != nil {
		return fmt.Errorf("list files in golden directory %q: %w", goldenDir, err)
	}
	gotFiles, err := ListFilesRecursivelyAsList(gotDir)
	if err != nil {
		return fmt.Errorf("list files in got directory %q: %w", gotDir, err)
	}

	if !slices.Equal(goldenFiles, gotFiles) {
		return fmt.Errorf("file sets differ between golden and got directories: %v vs %v", goldenFiles, gotFiles)
	}
	return nil
}

// CompareDirectoryYAMLSemantics compares the YAML semantics of the files in two directories and returns an error if they differ.
func CompareDirectoryYAMLSemantics(goldenDir, gotDir string) error {
	if err := CompareDirectoryFileSets(goldenDir, gotDir); err != nil {
		return err
	}

	relativeFilePaths, err := ListFilesRecursivelyAsList(goldenDir)
	if err != nil {
		return fmt.Errorf("list files in golden directory %q: %w", goldenDir, err)
	}

	for _, relativeFilePath := range relativeFilePaths {
		goldenPath := filepath.Join(goldenDir, relativeFilePath)
		gotPath := filepath.Join(gotDir, relativeFilePath)
		goldenBytes, err := os.ReadFile(goldenPath)
		if err != nil {
			return fmt.Errorf("read golden file %q: %w", goldenPath, err)
		}
		gotBytes, err := os.ReadFile(gotPath)
		if err != nil {
			return fmt.Errorf("read got file %q: %w", gotPath, err)
		}
		if err := compareYAMLFileBytes(relativeFilePath, goldenBytes, gotBytes); err != nil {
			return fmt.Errorf("compare YAML file %q: %w", relativeFilePath, err)
		}
	}
	return nil
}

// parseYAMLDocuments decodes every YAML document in data (including multi-document streams
// separated by ---) into Go values, typically map[string]any for mapping roots.
func parseYAMLDocuments(data []byte) ([]any, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []any
	for {
		var doc any
		if err := dec.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// compareYAMLFileBytes parses two YAML inputs and compares their decoded document streams.
// relPath is used only to provide context in returned errors.
func compareYAMLFileBytes(relPath string, golden, got []byte) error {
	goldenDocs, err := parseYAMLDocuments(golden)
	if err != nil {
		return fmt.Errorf("parse golden file %q: %w", relPath, err)
	}

	gotDocs, err := parseYAMLDocuments(got)
	if err != nil {
		return fmt.Errorf("parse got file %q: %w", relPath, err)
	}

	if !cmp.Equal(goldenDocs, gotDocs) {
		return fmt.Errorf("YAML differs in %q:\n%s", relPath, cmp.Diff(goldenDocs, gotDocs))
	}

	return nil
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
