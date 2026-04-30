package apply

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/internal/file"
	internalTransform "github.com/konveyor/crane/internal/transform"
	"github.com/sirupsen/logrus"
	yamlv3 "gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// KustomizeApplier applies transformations using kubectl kustomize
type KustomizeApplier struct {
	Log          *logrus.Logger
	TransformDir string
	OutputDir    string
}

// ApplySingleStage applies a single transform stage to produce output
func (k *KustomizeApplier) ApplySingleStage(stageName string) error {
	opts := file.PathOpts{
		TransformDir: k.TransformDir,
		OutputDir:    k.OutputDir,
	}

	stageDir := opts.GetStageDir(stageName)

	// Verify stage exists
	if _, err := os.Stat(stageDir); os.IsNotExist(err) {
		return fmt.Errorf("stage directory does not exist: %s", stageDir)
	}

	// Verify kustomization.yaml exists
	kustomizationPath := opts.GetKustomizationPath(stageName)
	if _, err := os.Stat(kustomizationPath); os.IsNotExist(err) {
		return fmt.Errorf("kustomization.yaml not found in stage: %s", stageName)
	}

	// Run kubectl kustomize build
	k.Log.Infof("Building stage: %s", stageName)
	output, err := k.runKustomizeBuild(stageDir)
	if err != nil {
		return fmt.Errorf("kubectl kustomize build failed for stage %s: %w", stageName, err)
	}

	// Write output to output directory
	outputPath := filepath.Join(k.OutputDir, stageName+".yaml")
	if err := os.MkdirAll(k.OutputDir, 0700); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if err := os.WriteFile(outputPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	k.Log.Infof("Successfully applied stage %s to %s", stageName, outputPath)
	return nil
}

// ApplyMultiStage applies a multi-stage transform pipeline
// For each stage, it regenerates the .work/<stage>/output/ directory by running kustomize
func (k *KustomizeApplier) ApplyMultiStage(stageSelector internalTransform.StageSelector) error {
	// Discover stages
	stages, err := internalTransform.DiscoverStages(k.TransformDir)
	if err != nil {
		return fmt.Errorf("failed to discover stages: %w", err)
	}

	// Filter stages
	selectedStages := internalTransform.FilterStages(stages, stageSelector)
	if len(selectedStages) == 0 {
		return fmt.Errorf("no stages found matching selector")
	}

	opts := file.PathOpts{
		TransformDir: k.TransformDir,
		OutputDir:    k.OutputDir,
	}

	// Process each stage sequentially to regenerate .work directories
	k.Log.Infof("Processing %d stage(s) sequentially to regenerate .work outputs", len(selectedStages))

	for i, stage := range selectedStages {
		k.Log.Infof("Processing stage %d/%d: %s", i+1, len(selectedStages), stage.DirName)

		// Run kubectl kustomize build on this stage
		stageDir := opts.GetStageDir(stage.DirName)
		output, err := k.runKustomizeBuild(stageDir)
		if err != nil {
			return fmt.Errorf("kubectl kustomize build failed for stage %s: %w", stage.DirName, err)
		}

		// Parse output into resources
		resources, err := file.ParseMultiDocYAML(output)
		if err != nil {
			return fmt.Errorf("failed to parse kustomize output for stage %s: %w", stage.DirName, err)
		}

		k.Log.Debugf("Stage %s: produced %d resource(s)", stage.DirName, len(resources))

		// Write resources to .work/<stage>/output/ directory
		stageOutputDir := opts.GetStageOutputDir(stage.DirName)
		if err := k.writeResourcesToDirectory(resources, stageOutputDir); err != nil {
			return fmt.Errorf("failed to write stage %s output to .work: %w", stage.DirName, err)
		}

		k.Log.Debugf("Stage %s: wrote output to %s", stage.DirName, stageOutputDir)
	}

	// Get the last (final) stage output
	lastStage := selectedStages[len(selectedStages)-1]
	k.Log.Infof("Applying final stage: %s", lastStage.DirName)

	// Run kubectl kustomize build on the last stage (again, to get bytes for output.yaml)
	output, err := k.runKustomizeBuild(lastStage.Path)
	if err != nil {
		return fmt.Errorf("kubectl kustomize build failed for stage %s: %w", lastStage.DirName, err)
	}

	// Write to output.yaml (single file with all resources)
	outputPath := filepath.Join(k.OutputDir, "output.yaml")
	if err := os.MkdirAll(k.OutputDir, 0700); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if err := os.WriteFile(outputPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	k.Log.Infof("Successfully applied final stage to %s", outputPath)

	// Split into individual resource files organized by namespace
	// This creates output/resources/<namespace>/<Kind>_<namespace>_<name>.yaml
	if err := k.splitMultiDocYAMLToFiles(output); err != nil {
		return fmt.Errorf("failed to split output into individual files: %w", err)
	}

	return nil
}

// runKustomizeBuild executes kubectl kustomize or oc kustomize on a directory
func (k *KustomizeApplier) runKustomizeBuild(dir string) ([]byte, error) {
	kustomizeCmd := file.GetKustomizeCommand()
	cmd := exec.Command(kustomizeCmd, "kustomize", dir)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	k.Log.Debugf("Running: %s kustomize %s", kustomizeCmd, dir)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("command failed: %w\nstderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// ValidateKubectlAvailable checks if kubectl or oc command is available
func ValidateKubectlAvailable() error {
	// Try kubectl first
	cmd := exec.Command("kubectl", "version", "--client")
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Fallback to oc
	cmd = exec.Command("oc", "version", "--client")
	if err := cmd.Run(); err == nil {
		return nil
	}

	return fmt.Errorf("neither kubectl nor oc found or executable")
}

// writeResourcesToDirectory writes resources as individual YAML files to a directory
func (k *KustomizeApplier) writeResourcesToDirectory(resources []unstructured.Unstructured, outputDir string) error {
	// Create output directory
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", outputDir, err)
	}

	// Write each resource to its own file
	for _, resource := range resources {
		// Generate filename using shared helper
		filename := file.GetResourceFilename(resource)
		filePath := filepath.Join(outputDir, filename)

		// Marshal resource to YAML
		yamlBytes, err := yaml.Marshal(resource.Object)
		if err != nil {
			return fmt.Errorf("failed to marshal resource %s: %w", filename, err)
		}

		// Write to file
		if err := os.WriteFile(filePath, yamlBytes, 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", filePath, err)
		}
	}

	k.Log.Debugf("Wrote %d resource(s) to %s", len(resources), outputDir)
	return nil
}

// splitMultiDocYAMLToFiles splits a multi-document YAML into individual resource files
// This maintains backward compatibility with tests/tools expecting separate files
func (k *KustomizeApplier) splitMultiDocYAMLToFiles(yamlData []byte) error {
	// Parse multi-document YAML
	decoder := yamlv3.NewDecoder(strings.NewReader(string(yamlData)))

	for {
		var doc any
		err := decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to decode YAML document: %w", err)
		}

		// Skip empty documents
		if doc == nil {
			continue
		}

		// Convert to YAML bytes with 2-space indentation
		var buf bytes.Buffer
		encoder := yamlv3.NewEncoder(&buf)
		encoder.SetIndent(2) // Set 2-space indentation
		if err := encoder.Encode(doc); err != nil {
			return fmt.Errorf("failed to encode YAML document: %w", err)
		}
		encoder.Close() // Flush the encoder
		docBytes := buf.Bytes()

		// Convert YAML to JSON to extract metadata
		jsonData, err := yaml.YAMLToJSON(docBytes)
		if err != nil {
			return fmt.Errorf("failed to convert YAML to JSON: %w", err)
		}

		// Unmarshal into unstructured to get resource identity
		u := unstructured.Unstructured{}
		if err := u.UnmarshalJSON(jsonData); err != nil {
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
			return fmt.Errorf("failed to create resource directory %s: %w", resourceDir, err)
		}

		// Write individual file using shared helper for consistency
		filename := file.GetResourceFilename(u)
		filePath := filepath.Join(resourceDir, filename)

		if err := os.WriteFile(filePath, docBytes, 0644); err != nil {
			return fmt.Errorf("failed to write resource file %s: %w", filePath, err)
		}

		k.Log.Debugf("Wrote resource file: %s", filePath)
	}

	return nil
}
