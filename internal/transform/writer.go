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
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// KustomizeWriter handles writing transform artifacts to Kustomize layout
type KustomizeWriter struct {
	opts      file.PathOpts
	stageName string
	log       *logrus.Logger
}

// NewKustomizeWriter creates a new KustomizeWriter for a specific stage
func NewKustomizeWriter(opts file.PathOpts, stageName string, log *logrus.Logger) *KustomizeWriter {
	return &KustomizeWriter{
		opts:      opts,
		stageName: stageName,
		log:       log,
	}
}

// WriteStage writes all artifacts for a stage to disk
func (w *KustomizeWriter) WriteStage(artifacts []cranelib.TransformArtifact, force bool) error {
	stageDir := w.opts.GetStageDir(w.stageName)

	// Handle directory preparation based on force flag
	if force {
		// Force mode - remove existing stage directory completely and recreate
		if err := os.RemoveAll(stageDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove existing stage directory: %w", err)
		}
	} else {
		// Safe mode - fail if stage directory is not empty
		if err := w.checkStageDirectory(stageDir); err != nil {
			return err
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
	// Use maps to deduplicate resources by canonical ID
	allResourcesMap := make(map[string]unstructured.Unstructured)
	whiteoutStatusMap := make(map[string]bool) // tracks whether resource is whiteout
	activeResourcesMap := make(map[string]unstructured.Unstructured)
	var patches []kustomize.Patch

	for _, artifact := range artifacts {
		resourceID := getResourceID(artifact.Resource)

		// Check for duplicates
		if _, exists := allResourcesMap[resourceID]; exists {
			// Duplicate detected - determine which to keep
			existingIsWhiteout := whiteoutStatusMap[resourceID]

			if existingIsWhiteout && !artifact.HaveWhiteOut {
				// Replace whiteout with non-whiteout
				w.log.Warnf("Duplicate resource %s: replacing whiteout with active resource", resourceID)
				allResourcesMap[resourceID] = artifact.Resource
				whiteoutStatusMap[resourceID] = artifact.HaveWhiteOut
				// Update active resources map
				delete(activeResourcesMap, resourceID) // remove old entry if any
				activeResourcesMap[resourceID] = artifact.Resource
			} else if !existingIsWhiteout && artifact.HaveWhiteOut {
				// Keep non-whiteout, skip whiteout duplicate
				w.log.Warnf("Duplicate resource %s: keeping active resource, ignoring whiteout duplicate", resourceID)
				continue
			} else {
				// Both same type - last one wins, but log warning
				w.log.Warnf("Duplicate resource %s (both %s): last occurrence will be used",
					resourceID, map[bool]string{true: "whiteout", false: "active"}[artifact.HaveWhiteOut])
				allResourcesMap[resourceID] = artifact.Resource
				whiteoutStatusMap[resourceID] = artifact.HaveWhiteOut
			}
		} else {
			// First occurrence - store it
			allResourcesMap[resourceID] = artifact.Resource
			whiteoutStatusMap[resourceID] = artifact.HaveWhiteOut
		}

		// Track whiteout status - whiteout resources don't get active references or patches
		if artifact.HaveWhiteOut {
			continue
		}

		// Non-whiteout resources are active
		activeResourcesMap[resourceID] = artifact.Resource

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

	// Convert maps to slices for writing
	var allResources []unstructured.Unstructured
	for _, res := range allResourcesMap {
		allResources = append(allResources, res)
	}

	// Build a set of active (non-whiteout) resources for quick lookup
	activeResourceIDs := make(map[string]bool)
	for id := range activeResourcesMap {
		activeResourceIDs[id] = true
	}

	var resourcePaths []string
	var whiteoutComments []string

	// Write each resource to its own file (similar to export structure)
	for _, resource := range allResources {
		filename := file.GetResourceFilename(resource)
		fullPath := filepath.Join(resourcesDir, filename)

		// Write individual resource file
		yamlBytes, err := yaml.Marshal(resource.Object)
		if err != nil {
			return fmt.Errorf("failed to marshal resource %s to YAML: %w", filename, err)
		}
		if err := os.WriteFile(fullPath, yamlBytes, 0644); err != nil {
			return fmt.Errorf("failed to write resource file %s: %w", filename, err)
		}

		// Check if this resource is active or whiteout
		resourceID := getResourceID(resource)
		if activeResourceIDs[resourceID] {
			resourcePaths = append(resourcePaths, filepath.Join("resources", filename))
		} else {
			// This resource is whiteout - add comment
			whiteoutComments = append(whiteoutComments, fmt.Sprintf("# - resources/%s", filename))
		}
	}

	// Sort resourcePaths for deterministic kustomization.yaml generation
	sort.Strings(resourcePaths)

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

// getResourceID returns a unique identifier for a resource
// Format: kind/namespace/name or kind/name for cluster-scoped
func getResourceID(resource unstructured.Unstructured) string {
	kind := resource.GetKind()
	namespace := resource.GetNamespace()
	name := resource.GetName()

	if namespace != "" {
		return fmt.Sprintf("%s/%s/%s", kind, namespace, name)
	}
	return fmt.Sprintf("%s/%s", kind, name)
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
