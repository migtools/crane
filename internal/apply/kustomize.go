package apply

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/internal/file"
	"github.com/konveyor/crane/internal/kustomize"
	internalTransform "github.com/konveyor/crane/internal/transform"
	"github.com/sirupsen/logrus"
	yamlv3 "gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// KustomizeApplier applies transformations using embedded kustomize
type KustomizeApplier struct {
	Log               *logrus.Logger
	TransformDir      string
	OutputDir         string
	KustomizeArgs     []string
	SkipClusterScoped bool
	Ordered           bool // Enable ordered resource filenames with dependency-aware prefixes
}

// ApplySingleStage applies a single transform stage to produce output
func (k *KustomizeApplier) ApplySingleStage(stageName string) error {
	if k.Log == nil {
		k.Log = logrus.StandardLogger()
	}
	opts := file.PathOpts{
		TransformDir: k.TransformDir,
		OutputDir:    k.OutputDir,
	}

	stageDir := opts.GetStageDir(stageName)

	// Verify stage exists
	if _, err := os.Stat(stageDir); os.IsNotExist(err) {
		k.Log.Debugf("Stage directory does not exist: %s", stageDir)
		return fmt.Errorf("stage directory does not exist: %s", stageDir)
	}

	// Verify kustomization.yaml exists
	kustomizationPath := opts.GetKustomizationPath(stageName)
	if _, err := os.Stat(kustomizationPath); os.IsNotExist(err) {
		k.Log.Debugf("kustomization.yaml not found in stage: %s", stageName)
		return fmt.Errorf("kustomization.yaml not found in stage: %s", stageName)
	}

	// Run kustomize build
	k.Log.Infof("Building stage: %s", stageName)
	output, err := k.runKustomizeBuild(stageDir)
	if err != nil {
		k.Log.Debugf("Kustomize build failed for stage %s: %v", stageName, err)
		return fmt.Errorf("kustomize build failed for stage %s: %w", stageName, err)
	}

	// Filter cluster-scoped resources if requested
	if k.SkipClusterScoped {
		output, err = k.filterClusterScopedResources(output)
		if err != nil {
			k.Log.Debugf("Failed to filter cluster-scoped resources in stage %s: %v", stageName, err)
			return fmt.Errorf("failed to filter cluster-scoped resources: %w", err)
		}
	}

	// Write output to output directory
	outputPath := filepath.Join(k.OutputDir, stageName+".yaml")
	if err := os.MkdirAll(k.OutputDir, 0700); err != nil {
		k.Log.Debugf("Failed to create output directory %s: %v", k.OutputDir, err)
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if err := os.WriteFile(outputPath, output, 0644); err != nil {
		k.Log.Debugf("Failed to write output file %s: %v", outputPath, err)
		return fmt.Errorf("failed to write output file: %w", err)
	}

	k.Log.Infof("Successfully applied stage %s to %s", stageName, outputPath)
	return nil
}

// ApplyMultiStage applies a multi-stage transform pipeline
func (k *KustomizeApplier) ApplyMultiStage(stageSelector internalTransform.StageSelector) error {
	if k.Log == nil {
		k.Log = logrus.StandardLogger()
	}
	// Discover stages
	stages, err := internalTransform.DiscoverStages(k.TransformDir)
	if err != nil {
		k.Log.Debugf("Failed to discover stages in %s: %v", k.TransformDir, err)
		return fmt.Errorf("failed to discover stages: %w", err)
	}

	// Filter stages
	selectedStages := internalTransform.FilterStages(stages, stageSelector)
	if len(selectedStages) == 0 {
		k.Log.Debugf("No stages found matching selector in %s", k.TransformDir)
		return fmt.Errorf("no stages found matching selector")
	}

	// Get the last (final) stage
	lastStage := selectedStages[len(selectedStages)-1]

	k.Log.Infof("Applying final stage: %s", lastStage.DirName)

	// Run kustomize build on the last stage
	output, err := k.runKustomizeBuild(lastStage.Path)
	if err != nil {
		k.Log.Debugf("Kustomize build failed for stage %s: %v", lastStage.DirName, err)
		return fmt.Errorf("kustomize build failed for stage %s: %w", lastStage.DirName, err)
	}

	// Filter cluster-scoped resources if requested
	if k.SkipClusterScoped {
		output, err = k.filterClusterScopedResources(output)
		if err != nil {
			k.Log.Debugf("Failed to filter cluster-scoped resources in stage %s: %v", lastStage.DirName, err)
			return fmt.Errorf("failed to filter cluster-scoped resources: %w", err)
		}
	}

	// Write to output.yaml (single file with all resources)
	outputPath := filepath.Join(k.OutputDir, "output.yaml")
	if err := os.MkdirAll(k.OutputDir, 0700); err != nil {
		k.Log.Debugf("Failed to create output directory %s: %v", k.OutputDir, err)
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if err := os.WriteFile(outputPath, output, 0644); err != nil {
		k.Log.Debugf("Failed to write output file %s: %v", outputPath, err)
		return fmt.Errorf("failed to write output file: %w", err)
	}

	k.Log.Infof("Successfully applied final stage to %s", outputPath)

	// Split into individual resource files organized by namespace
	// This creates output/resources/<namespace>/<Kind>_<namespace>_<name>.yaml
	if err := k.splitMultiDocYAMLToFiles(output); err != nil {
		k.Log.Debugf("Failed to split output into individual files: %v", err)
		return fmt.Errorf("failed to split output into individual files: %w", err)
	}

	return nil
}

// runKustomizeBuild runs embedded kustomize on a directory
func (k *KustomizeApplier) runKustomizeBuild(dir string) ([]byte, error) {
	runner := &kustomize.Runner{
		Log:  k.Log,
		Args: k.KustomizeArgs,
	}
	return runner.Build(dir)
}

// filterClusterScopedResources removes cluster-scoped resources from a multi-document YAML stream
func (k *KustomizeApplier) filterClusterScopedResources(yamlData []byte) ([]byte, error) {
	decoder := yamlv3.NewDecoder(strings.NewReader(string(yamlData)))
	var result bytes.Buffer
	first := true

	for {
		var doc interface{}
		err := decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			k.Log.Debugf("Failed to decode YAML document: %v", err)
			return nil, fmt.Errorf("failed to decode YAML document: %w", err)
		}
		if doc == nil {
			k.Log.Debugf("Skipping empty YAML document")
			continue
		}

		var buf bytes.Buffer
		encoder := yamlv3.NewEncoder(&buf)
		encoder.SetIndent(2)
		if err := encoder.Encode(doc); err != nil {
			k.Log.Debugf("Failed to encode YAML document: %v", err)
			return nil, fmt.Errorf("failed to encode YAML document: %w", err)
		}
		if err := encoder.Close(); err != nil {
			k.Log.Debugf("Failed to close YAML encoder: %v", err)
			return nil, fmt.Errorf("failed to close YAML encoder: %w", err)
		}
		docBytes := buf.Bytes()

		jsonData, err := yaml.YAMLToJSON(docBytes)
		if err != nil {
			k.Log.Debugf("Failed to convert YAML to JSON: %v", err)
			return nil, fmt.Errorf("failed to convert YAML to JSON: %w", err)
		}

		u := unstructured.Unstructured{}
		if err := u.UnmarshalJSON(jsonData); err != nil {
			k.Log.Debugf("Failed to unmarshal resource: %v", err)
			return nil, fmt.Errorf("failed to unmarshal resource: %w", err)
		}

		if u.GetNamespace() == "" {
			k.Log.Infof("Skipping cluster-scoped resource %s/%s (--skip-cluster-scoped)", u.GetKind(), u.GetName())
			continue
		}

		if !first {
			result.WriteString("---\n")
		}
		result.Write(docBytes)
		first = false
	}

	return result.Bytes(), nil
}

// splitMultiDocYAMLToFiles splits a multi-document YAML into individual resource files
// This maintains backward compatibility with tests/tools expecting separate files
func (k *KustomizeApplier) splitMultiDocYAMLToFiles(yamlData []byte) error {
	// Parse multi-document YAML
	decoder := yamlv3.NewDecoder(strings.NewReader(string(yamlData)))

	for {
		var doc interface{}
		err := decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			k.Log.Debugf("Failed to decode YAML document: %v", err)
			return fmt.Errorf("failed to decode YAML document: %w", err)
		}

		// Skip empty documents
		if doc == nil {
			k.Log.Debugf("Skipping empty YAML document")
			continue
		}

		// Convert to YAML bytes with 2-space indentation
		var buf bytes.Buffer
		encoder := yamlv3.NewEncoder(&buf)
		encoder.SetIndent(2) // Set 2-space indentation
		if err := encoder.Encode(doc); err != nil {
			k.Log.Debugf("Failed to encode YAML document: %v", err)
			return fmt.Errorf("failed to encode YAML document: %w", err)
		}
		if err := encoder.Close(); err != nil {
			k.Log.Debugf("Failed to close YAML encoder: %v", err)
			return fmt.Errorf("failed to close YAML encoder: %w", err)
		}
		docBytes := buf.Bytes()

		// Convert YAML to JSON to extract metadata
		jsonData, err := yaml.YAMLToJSON(docBytes)
		if err != nil {
			k.Log.Debugf("Failed to convert YAML to JSON: %v", err)
			return fmt.Errorf("failed to convert YAML to JSON: %w", err)
		}

		// Unmarshal into unstructured to get resource identity
		u := unstructured.Unstructured{}
		if err := u.UnmarshalJSON(jsonData); err != nil {
			k.Log.Debugf("Failed to unmarshal resource: %v", err)
			return fmt.Errorf("failed to unmarshal resource: %w", err)
		}

		// Generate filename: Kind_namespace_name.yaml
		kind := u.GetKind()
		namespace := u.GetNamespace()
		name := u.GetName()

		if kind == "" || name == "" {
			k.Log.Warnf("Skipping resource with missing kind or name")
			continue
		}

		// Create directory structure: output/resources/namespace/
		var resourceDir string
		if namespace != "" {
			resourceDir = filepath.Join(k.OutputDir, "resources", namespace)
		} else {
			resourceDir = filepath.Join(k.OutputDir, "resources", "_cluster")
		}

		if err := os.MkdirAll(resourceDir, 0700); err != nil {
			k.Log.Debugf("Failed to create resource directory %s: %v", resourceDir, err)
			return fmt.Errorf("failed to create resource directory %s: %w", resourceDir, err)
		}

		// Write individual file - use ordered filename if requested
		var filename string
		if k.Ordered {
			filename = file.GetOrderedResourceFilename(u)
		} else {
			filename = file.GetResourceFilename(u)
		}
		filePath := filepath.Join(resourceDir, filename)

		if err := os.WriteFile(filePath, docBytes, 0644); err != nil {
			k.Log.Debugf("Failed to write resource file %s: %v", filePath, err)
			return fmt.Errorf("failed to write resource file %s: %w", filePath, err)
		}

		k.Log.Debugf("Wrote resource file: %s", filePath)
	}

	return nil
}
