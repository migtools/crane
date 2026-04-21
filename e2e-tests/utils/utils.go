package utils

import (
	"bytes"
	"encoding/json"
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

// ListFilesRecursively returns a human-readable, newline-delimited list of files
// under dir using relative paths (prefixed with "  - ").
// It returns "  (no files)" when dir contains no files.
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

// ListFilesRecursivelyAsList walks dir recursively and returns sorted relative
// file paths. Directory entries are excluded.
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

// ReadTestdataFile reads a file from e2e-tests/testdata relative to this package.
// filename should be a path relative to that testdata directory.
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

// CompareDirectoryYAMLSemantics compares YAML semantics for all matching files in
// two directories using strict file-set equality and no export-specific normalization.
// This is intended for stable outputs (for example transform/output artifacts).
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

// CompareDirectoryYAMLSemanticsExport compares export YAML semantics using
// normalization and resource-identity grouping instead of strict filename matching.
// This avoids false diffs caused by unstable export filenames (for example, Pod or
// EndpointSlice names with generated suffixes encoded in file paths).
// When multiple documents map to the same identity, both sides are compared as
// multisets after canonical JSON normalization.
func CompareDirectoryYAMLSemanticsExport(goldenDir, gotDir string) error {
	goldenIndex, err := buildNormalizedExportIndex(goldenDir)
	if err != nil {
		return fmt.Errorf("index golden export directory %q: %w", goldenDir, err)
	}
	gotIndex, err := buildNormalizedExportIndex(gotDir)
	if err != nil {
		return fmt.Errorf("index got export directory %q: %w", gotDir, err)
	}

	goldenIDs := make([]string, 0, len(goldenIndex))
	for identity := range goldenIndex {
		goldenIDs = append(goldenIDs, identity)
	}
	sort.Strings(goldenIDs)

	gotIDs := make([]string, 0, len(gotIndex))
	for identity := range gotIndex {
		gotIDs = append(gotIDs, identity)
	}
	sort.Strings(gotIDs)

	if !slices.Equal(goldenIDs, gotIDs) {
		return fmt.Errorf("resource identity sets differ between golden and got directories: %v vs %v", goldenIDs, gotIDs)
	}

	for _, identity := range goldenIDs {
		goldenEntries := goldenIndex[identity]
		gotEntries := gotIndex[identity]
		goldenDocs, err := canonicalizeDocs(goldenEntries)
		if err != nil {
			return fmt.Errorf("canonicalize golden docs for identity %q: %w", identity, err)
		}
		gotDocs, err := canonicalizeDocs(gotEntries)
		if err != nil {
			return fmt.Errorf("canonicalize got docs for identity %q: %w", identity, err)
		}
		if !cmp.Equal(goldenDocs, gotDocs) {
			return fmt.Errorf("YAML differs for identity %q:\n%s", identity, cmp.Diff(goldenDocs, gotDocs))
		}
	}
	return nil
}

type exportIndexedDoc struct {
	doc    any
	source string
}

// buildNormalizedExportIndex reads YAML-like files under dir, extracts a stable
// identity from each document, normalizes unstable fields, and groups documents
// by identity.
func buildNormalizedExportIndex(dir string) (map[string][]exportIndexedDoc, error) {
	relativeFilePaths, err := ListFilesRecursivelyAsList(dir)
	if err != nil {
		return nil, fmt.Errorf("list files in directory %q: %w", dir, err)
	}

	index := make(map[string][]exportIndexedDoc)
	for _, relativeFilePath := range relativeFilePaths {
		if !LooksLikeYAMLFile(relativeFilePath) {
			continue
		}

		fullPath := filepath.Join(dir, relativeFilePath)
		fileBytes, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("read file %q: %w", fullPath, err)
		}

		docs, err := parseYAMLDocuments(fileBytes)
		if err != nil {
			return nil, fmt.Errorf("parse file %q: %w", relativeFilePath, err)
		}

		for i, doc := range docs {
			identity, err := extractResourceIdentity(doc)
			if err != nil {
				return nil, fmt.Errorf("extract identity for %q doc #%d: %w", relativeFilePath, i+1, err)
			}
			normalized := normalizeUnstableFields(doc)
			index[identity] = append(index[identity], exportIndexedDoc{
				doc:    normalized,
				source: relativeFilePath,
			})
		}
	}
	return index, nil
}

// canonicalizeDocs marshals normalized docs to JSON strings and sorts them so
// order-insensitive multiset comparison is deterministic.
func canonicalizeDocs(entries []exportIndexedDoc) ([]string, error) {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		b, err := json.Marshal(entry.doc)
		if err != nil {
			return nil, fmt.Errorf("marshal doc from %q: %w", entry.source, err)
		}
		out = append(out, string(b))
	}
	sort.Strings(out)
	return out, nil
}

// extractResourceIdentity returns a stable resource key from a decoded YAML document.
func extractResourceIdentity(doc any) (string, error) {
	root, ok := doc.(map[string]any)
	if !ok {
		return "", fmt.Errorf("expected map[string]any root but got %T", doc)
	}

	apiVersion, _ := root["apiVersion"].(string)
	kind, _ := root["kind"].(string)
	if apiVersion == "" || kind == "" {
		return "", fmt.Errorf("missing apiVersion or kind")
	}

	metadata, ok := root["metadata"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("missing metadata object")
	}
	namespace, _ := metadata["namespace"].(string)

	// Pod names have generated suffixes; use stable owning controller name when present.
	if kind == "Pod" {
		ownerReferences, _ := metadata["ownerReferences"].([]any)
		var firstOwnerName string
		for _, ref := range ownerReferences {
			owner, ok := ref.(map[string]any)
			if !ok {
				continue
			}
			ownerName, _ := owner["name"].(string)
			if ownerName == "" {
				continue
			}
			if firstOwnerName == "" {
				firstOwnerName = ownerName
			}
			if controller, _ := owner["controller"].(bool); controller {
				return fmt.Sprintf("%s|%s|%s|owner:%s", apiVersion, kind, namespace, ownerName), nil
			}
		}
		if firstOwnerName != "" {
			return fmt.Sprintf("%s|%s|%s|owner:%s", apiVersion, kind, namespace, firstOwnerName), nil
		}
	}

	// EndpointSlice names are generated; service label is stable.
	if kind == "EndpointSlice" {
		labels, _ := metadata["labels"].(map[string]any)
		if labels != nil {
			if serviceName, _ := labels["kubernetes.io/service-name"].(string); serviceName != "" {
				return fmt.Sprintf("%s|%s|%s|service:%s", apiVersion, kind, namespace, serviceName), nil
			}
		}
	}

	name, _ := metadata["name"].(string)
	if name == "" {
		return "", fmt.Errorf("missing metadata.name")
	}

	return fmt.Sprintf("%s|%s|%s|%s", apiVersion, kind, namespace, name), nil
}

// parseYAMLDocuments decodes all YAML documents in data (including multi-document
// streams separated by "---") into generic Go values.
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

// compareYAMLFileBytes parses two YAML inputs and compares their decoded document
// streams without export-specific normalization.
// relPath is used only for error context.
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

// normalizeUnstableFields removes selected unstable fields from a decoded YAML
// document tree (maps/slices/scalars) for stable export comparisons.
// It also performs small kind/name-specific normalization for known
// cluster-generated values.
func normalizeUnstableFields(doc any) any {
	normalized := normalizeWithPath(doc, nil)
	root, ok := normalized.(map[string]any)
	if !ok {
		return normalized
	}

	kind, _ := root["kind"].(string)
	metadata, ok := root["metadata"].(map[string]any)
	if !ok {
		return normalized
	}

	if kind == "ConfigMap" {
		name, _ := metadata["name"].(string)
		if name == "kube-root-ca.crt" {
			if data, ok := root["data"].(map[string]any); ok {
				delete(data, "ca.crt")
			}
			return normalized
		}
	}

	if kind != "Pod" && kind != "EndpointSlice" {
		return normalized
	}

	// Pod and EndpointSlice names include generated suffixes in export output.
	delete(metadata, "name")
	if kind == "Pod" {
		normalizePodServiceAccountVolumeNames(root)
	}
	return normalized
}

// normalizePodServiceAccountVolumeNames canonicalizes generated
// kube-api-access-<suffix> volume names in Pod specs and mounts.
func normalizePodServiceAccountVolumeNames(root map[string]any) {
	spec, ok := root["spec"].(map[string]any)
	if !ok {
		return
	}
	const (
		generatedPrefix = "kube-api-access-"
		canonicalName   = "kube-api-access"
	)

	canonicalizedVolumeNames := make(map[string]string)
	volumes, _ := spec["volumes"].([]any)
	for _, volumeValue := range volumes {
		volume, ok := volumeValue.(map[string]any)
		if !ok {
			continue
		}
		name, _ := volume["name"].(string)
		if !strings.HasPrefix(name, generatedPrefix) {
			continue
		}
		if _, projected := volume["projected"].(map[string]any); projected {
			canonicalizedVolumeNames[name] = canonicalName
			volume["name"] = canonicalName
		}
	}

	normalizeMountNames := func(containers []any) {
		for _, containerValue := range containers {
			container, ok := containerValue.(map[string]any)
			if !ok {
				continue
			}
			mounts, _ := container["volumeMounts"].([]any)
			for _, mountValue := range mounts {
				mount, ok := mountValue.(map[string]any)
				if !ok {
					continue
				}
				name, _ := mount["name"].(string)
				if replacement, ok := canonicalizedVolumeNames[name]; ok {
					mount["name"] = replacement
				}
			}
		}
	}

	if containers, _ := spec["containers"].([]any); containers != nil {
		normalizeMountNames(containers)
	}
	if initContainers, _ := spec["initContainers"].([]any); initContainers != nil {
		normalizeMountNames(initContainers)
	}
}

// normalizeWithPath recursively copies and normalizes a decoded YAML value while
// tracking the current key path for field-drop decisions.
// For list elements, path is unchanged because list indices are not part of
// field-selection rules.
func normalizeWithPath(value any, path []string) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, child := range v {
			if shouldDropField(path, k) {
				continue
			}
			nextPath := append(append([]string{}, path...), k)
			out[k] = normalizeWithPath(child, nextPath)
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, normalizeWithPath(item, path))
		}
		return out
	case string, int, float64, bool, nil:
		return v
	}
	return value
}

// shouldDropField reports whether key should be removed at the given map path
// during export normalization.
// Path examples:
//   - [] + "status" -> top-level status
//   - ["metadata"] + "uid" -> metadata.uid
//   - ["subsets","addresses"] + "ip" -> subsets[].addresses[].ip
func shouldDropField(path []string, key string) bool {
	if len(path) == 0 && key == "status" {
		return true
	}
	if len(path) == 1 && path[0] == "metadata" {
		switch key {
		case "uid", "resourceVersion", "creationTimestamp", "managedFields", "generation":
			return true
		}
	} else if len(path) == 2 && path[0] == "metadata" && path[1] == "annotations" {
		switch key {
		case "endpoints.kubernetes.io/last-change-trigger-time":
			return true
		}
	} else if len(path) == 2 && path[0] == "metadata" && path[1] == "ownerReferences" {
		switch key {
		case "uid":
			return true
		}
	} else if len(path) == 1 && path[0] == "spec" {
		switch key {
		case "clusterIP", "clusterIPs", "volumeName":
			return true
		}
	} else if len(path) == 1 && path[0] == "endpoints" {
		switch key {
		case "addresses":
			return true
		}
	} else if len(path) == 2 && path[0] == "endpoints" && path[1] == "targetRef" {
		switch key {
		case "name", "uid":
			return true
		}
	} else if len(path) == 2 && path[0] == "subsets" && path[1] == "addresses" {
		switch key {
		case "ip":
			return true
		}
	} else if len(path) == 3 && path[0] == "subsets" && path[1] == "addresses" && path[2] == "targetRef" {
		switch key {
		case "name", "uid":
			return true
		}
	}
	return false
}
