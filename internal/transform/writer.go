package transform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	cranelib "github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane-lib/transform/kustomize"
	"github.com/konveyor/crane/internal/file"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// KustomizeWriter handles writing transform artifacts to Kustomize layout
type KustomizeWriter struct {
	opts      file.PathOpts
	stageName string
}

// NewKustomizeWriter creates a new KustomizeWriter for a specific stage
func NewKustomizeWriter(opts file.PathOpts, stageName string) *KustomizeWriter {
	return &KustomizeWriter{
		opts:      opts,
		stageName: stageName,
	}
}

// WriteStage writes all artifacts for a stage to disk
func (w *KustomizeWriter) WriteStage(artifacts []cranelib.TransformArtifact, force bool) error {
	stageDir := w.opts.GetStageDir(w.stageName)

	// Check if stage directory exists and is non-empty
	if !force {
		if err := w.checkStageDirectory(stageDir); err != nil {
			return err
		}
	} else {
		// Force is set, remove existing stage directory
		if err := os.RemoveAll(stageDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove existing stage directory: %w", err)
		}
	}

	// Create stage directories
	resourcesDir := w.opts.GetResourcesDir(w.stageName)
	patchesDir := w.opts.GetPatchesDir(w.stageName)

	if err := os.MkdirAll(resourcesDir, 0700); err != nil {
		return fmt.Errorf("failed to create resources directory: %w", err)
	}
	if err := os.MkdirAll(patchesDir, 0700); err != nil {
		return fmt.Errorf("failed to create patches directory: %w", err)
	}

	// Separate artifacts into whiteout and non-whiteout
	// ALL artifacts will be written to resources/, but only non-whiteout will be in kustomization.yaml
	var allResources []unstructured.Unstructured
	var activeResources []unstructured.Unstructured
	var patches []kustomize.Patch

	for _, artifact := range artifacts {
		// Store all resources (including whiteout) for complete snapshot
		allResources = append(allResources, artifact.Resource)

		// Track whiteout status - whiteout resources don't get active references or patches
		if artifact.HaveWhiteOut {
			continue
		}

		// Non-whiteout resources are active
		activeResources = append(activeResources, artifact.Resource)

		// Write patch if there are operations
		if len(artifact.Patches) > 0 {
			// Filter out remove operations for non-existent paths to prevent kubectl kustomize errors
			validPatches, err := filterValidRemoveOps(artifact.Resource, artifact.Patches)
			if err != nil {
				return fmt.Errorf("failed to filter patches for %s/%s/%s: %w",
					artifact.Target.Kind, artifact.Target.Namespace, artifact.Target.Name, err)
			}

			// Skip writing patch file if all operations were filtered out
			if len(validPatches) == 0 {
				continue
			}

			patchFilename := kustomize.GeneratePatchFilename(
				artifact.Target.Group,
				artifact.Target.Version,
				artifact.Target.Kind,
				artifact.Target.Name,
				artifact.Target.Namespace,
			)
			patchPath := filepath.Join(patchesDir, patchFilename)

			patchYAML, err := kustomize.SerializePatchToYAML(validPatches)
			if err != nil {
				return fmt.Errorf("failed to serialize patch for %s/%s/%s: %w",
					artifact.Target.Kind, artifact.Target.Namespace, artifact.Target.Name, err)
			}

			if err := os.WriteFile(patchPath, patchYAML, 0644); err != nil {
				return fmt.Errorf("failed to write patch file %s for %s/%s/%s: %w",
					patchPath, artifact.Target.Kind, artifact.Target.Namespace, artifact.Target.Name, err)
			}

			patches = append(patches, kustomize.Patch{
				Path: filepath.Join("patches", patchFilename),
				Target: kustomize.PatchTarget{
					Group:     artifact.Target.Group,
					Version:   artifact.Target.Version,
					Kind:      artifact.Target.Kind,
					Name:      artifact.Target.Name,
					Namespace: artifact.Target.Namespace,
				},
			})
		}
	}

	// Group ALL resources by type and write resource files (including whiteout)
	allGroups := cranelib.GroupResourcesByType(allResources)

	// Group only ACTIVE resources to determine what goes in kustomization.yaml
	activeGroups := cranelib.GroupResourcesByType(activeResources)
	activeTypeKeys := make(map[string]bool)
	for _, group := range activeGroups {
		activeTypeKeys[group.TypeKey] = true
	}

	var resourcePaths []string
	var whiteoutComments []string

	for _, resourceGroup := range allGroups {
		// Parse TypeKey to extract kind and group
		kind, group := parseTypeKey(resourceGroup.TypeKey)
		filename := kustomize.GetResourceTypeFilename(kind, group)
		fullPath := filepath.Join(resourcesDir, filename)

		// Write resource file (even for whiteout types - complete snapshot)
		if err := cranelib.WriteResourceTypeFile(fullPath, resourceGroup.Resources); err != nil {
			return fmt.Errorf("failed to write resource file %s: %w", filename, err)
		}

		// Only add to active resources list if NOT whiteout
		if activeTypeKeys[resourceGroup.TypeKey] {
			resourcePaths = append(resourcePaths, filepath.Join("resources", filename))
		} else {
			// This type is whiteout - add comment
			whiteoutComments = append(whiteoutComments, fmt.Sprintf("# - resources/%s", filename))
		}
	}

	// Generate and write kustomization.yaml with whiteout comments
	kustomizationYAML, err := w.generateKustomizationWithComments(resourcePaths, patches, whiteoutComments)
	if err != nil {
		return fmt.Errorf("failed to generate kustomization.yaml: %w", err)
	}

	kustomizationPath := w.opts.GetKustomizationPath(w.stageName)
	if err := os.WriteFile(kustomizationPath, kustomizationYAML, 0644); err != nil {
		return fmt.Errorf("failed to write kustomization.yaml: %w", err)
	}

	return nil
}

// parseTypeKey extracts kind and group from ResourceGroup's TypeKey
// Format: "<kind>" for core resources, "<kind>.<group>" for others
// Examples: "deployment" -> ("Deployment", ""), "route.route.openshift.io" -> ("Route", "route.openshift.io")
func parseTypeKey(typeKey string) (kind, group string) {
	// Check if typeKey contains a dot (indicating non-core resource)
	dotIndex := -1
	for i, ch := range typeKey {
		if ch == '.' {
			dotIndex = i
			break
		}
	}

	if dotIndex == -1 {
		// Core resource - capitalize first letter
		return capitalizeFirst(typeKey), ""
	}

	// Non-core resource - split on first dot
	kindPart := typeKey[:dotIndex]
	groupPart := typeKey[dotIndex+1:]
	return capitalizeFirst(kindPart), groupPart
}

// capitalizeFirst capitalizes the first letter of a string
func capitalizeFirst(s string) string {
	if s == "" {
		return ""
	}
	// Simple capitalization - assumes ASCII
	first := s[0]
	if first >= 'a' && first <= 'z' {
		return string(first-32) + s[1:]
	}
	return s
}

// filterValidRemoveOps filters out JSONPatch remove operations for paths that don't exist in the resource.
// This prevents kubectl kustomize from failing when trying to remove non-existent fields.
func filterValidRemoveOps(resource unstructured.Unstructured, patches jsonpatch.Patch) (jsonpatch.Patch, error) {
	if len(patches) == 0 {
		return patches, nil
	}

	// Convert resource to JSON for path checking
	resourceJSON, err := resource.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resource to JSON: %w", err)
	}

	var resourceMap map[string]interface{}
	if err := json.Unmarshal(resourceJSON, &resourceMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource JSON: %w", err)
	}

	var validPatches jsonpatch.Patch
	for _, op := range patches {
		// Get operation type and path using the Operation methods
		opType := op.Kind()
		path, err := op.Path()
		if err != nil {
			// If we can't get the path, keep the operation
			validPatches = append(validPatches, op)
			continue
		}

		// Only filter remove operations
		if opType != "remove" {
			validPatches = append(validPatches, op)
			continue
		}

		// Check if the path exists in the resource
		if pathExists(resourceMap, path) {
			validPatches = append(validPatches, op)
		}
		// If path doesn't exist, skip this remove operation (no-op)
	}

	return validPatches, nil
}

// pathExists checks if a JSON pointer path exists in the given data structure
func pathExists(data map[string]interface{}, path string) bool {
	if path == "" || path == "/" {
		return true
	}

	// Remove leading slash
	path = strings.TrimPrefix(path, "/")

	// Split path into segments
	segments := strings.Split(path, "/")

	var current interface{} = data
	for i, segment := range segments {
		// Unescape JSON pointer special characters
		segment = strings.ReplaceAll(segment, "~1", "/")
		segment = strings.ReplaceAll(segment, "~0", "~")

		// Handle based on current value type
		switch v := current.(type) {
		case map[string]interface{}:
			// Traverse map
			value, exists := v[segment]
			if !exists {
				return false
			}

			// If this is the last segment, we found it
			if i == len(segments)-1 {
				return true
			}

			current = value

		case []interface{}:
			// Traverse array - segment must be a valid numeric index
			// Per RFC 6901, array indices must not have leading zeros (except "0" itself)
			if segment == "" {
				return false
			}

			// Check for leading zero (invalid except for "0")
			if len(segment) > 1 && segment[0] == '0' {
				return false
			}

			// Parse numeric index
			index := 0
			for _, ch := range segment {
				if ch < '0' || ch > '9' {
					// Not a valid array index
					return false
				}
				index = index*10 + int(ch-'0')
			}

			// Check bounds
			if index >= len(v) {
				return false
			}

			// If this is the last segment, we found it
			if i == len(segments)-1 {
				return true
			}

			current = v[index]

		default:
			// Can't traverse further (primitive value or nil), but we haven't reached the end
			return false
		}
	}

	return true
}

// generateKustomizationWithComments creates a kustomization.yaml with human-readable whiteout comments
func (w *KustomizeWriter) generateKustomizationWithComments(resources []string, patches []kustomize.Patch, whiteoutComments []string) ([]byte, error) {
	// Generate base kustomization YAML
	baseYAML, err := kustomize.GenerateKustomization(resources, patches)
	if err != nil {
		return nil, err
	}

	// If no whiteout comments, return as-is
	if len(whiteoutComments) == 0 {
		return baseYAML, nil
	}

	// Append whiteout comments at the end (sorted for determinism)
	sortedComments := make([]string, len(whiteoutComments))
	copy(sortedComments, whiteoutComments)
	sort.Strings(sortedComments)

	var result strings.Builder
	result.Write(baseYAML)
	result.WriteString("\n# Whiteout resources are written to resources/ for complete snapshot\n")
	result.WriteString("# but excluded from active resources list above:\n")
	for _, comment := range sortedComments {
		result.WriteString(comment)
		result.WriteString("\n")
	}

	return []byte(result.String()), nil
}

// checkStageDirectory checks if a stage directory exists and is non-empty
// Returns an error if the directory exists and contains files (preventing accidental overwrites)
func (w *KustomizeWriter) checkStageDirectory(stageDir string) error {
	// Check if directory exists
	info, err := os.Stat(stageDir)
	if os.IsNotExist(err) {
		// Directory doesn't exist, safe to create
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to check stage directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("stage path exists but is not a directory: %s", stageDir)
	}

	// Check if directory is empty
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		return fmt.Errorf("failed to read stage directory: %w", err)
	}

	if len(entries) > 0 {
		return fmt.Errorf("stage directory %s is not empty (use --force to overwrite)", stageDir)
	}

	return nil
}
