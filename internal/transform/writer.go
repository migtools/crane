package transform

import (
	"fmt"
	"os"
	"path/filepath"

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

	// Remove existing stage directory if force is set
	if force {
		if err := os.RemoveAll(stageDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove existing stage directory: %w", err)
		}
	}

	// Create stage directories
	resourcesDir := w.opts.GetResourcesDir(w.stageName)
	patchesDir := w.opts.GetPatchesDir(w.stageName)

	if err := os.MkdirAll(resourcesDir, 0755); err != nil {
		return fmt.Errorf("failed to create resources directory: %w", err)
	}
	if err := os.MkdirAll(patchesDir, 0755); err != nil {
		return fmt.Errorf("failed to create patches directory: %w", err)
	}

	// Group resources and patches
	var resources []unstructured.Unstructured
	var patches []kustomize.Patch

	for _, artifact := range artifacts {
		// Skip whiteout resources
		if artifact.HaveWhiteOut {
			continue
		}

		resources = append(resources, artifact.Resource)

		// Write patch if there are operations
		if len(artifact.Patches) > 0 {
			patchFilename := kustomize.GeneratePatchFilename(
				artifact.Target.Group,
				artifact.Target.Version,
				artifact.Target.Kind,
				artifact.Target.Name,
				artifact.Target.Namespace,
			)
			patchPath := filepath.Join(patchesDir, patchFilename)

			patchYAML, err := kustomize.SerializePatchToYAML(artifact.Patches)
			if err != nil {
				return fmt.Errorf("failed to serialize patch: %w", err)
			}

			if err := os.WriteFile(patchPath, patchYAML, 0644); err != nil {
				return fmt.Errorf("failed to write patch file: %w", err)
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

	// Group resources by type and write resource files
	groups := cranelib.GroupResourcesByType(resources)
	var resourcePaths []string

	for _, resourceGroup := range groups {
		// Parse TypeKey to extract kind and group
		kind, group := parseTypeKey(resourceGroup.TypeKey)
		filename := kustomize.GetResourceTypeFilename(kind, group)
		fullPath := filepath.Join(resourcesDir, filename)

		if err := cranelib.WriteResourceTypeFile(fullPath, resourceGroup.Resources); err != nil {
			return fmt.Errorf("failed to write resource file %s: %w", filename, err)
		}

		resourcePaths = append(resourcePaths, filepath.Join("resources", filename))
	}

	// Generate and write kustomization.yaml
	kustomizationYAML, err := kustomize.GenerateKustomization(resourcePaths, patches)
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
