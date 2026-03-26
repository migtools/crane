package apply

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/konveyor/crane/internal/file"
	internalTransform "github.com/konveyor/crane/internal/transform"
	"github.com/sirupsen/logrus"
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
	if err := os.MkdirAll(k.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if err := os.WriteFile(outputPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	k.Log.Infof("Successfully applied stage %s to %s", stageName, outputPath)
	return nil
}

// ApplyMultiStage applies a multi-stage transform pipeline
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

	// Apply each stage
	for _, stage := range selectedStages {
		k.Log.Infof("Applying stage: %s", stage.DirName)

		// Run kubectl kustomize build
		output, err := k.runKustomizeBuild(stage.Path)
		if err != nil {
			return fmt.Errorf("kubectl kustomize build failed for stage %s: %w", stage.DirName, err)
		}

		// Write output
		outputPath := filepath.Join(k.OutputDir, stage.DirName+".yaml")
		if err := os.MkdirAll(k.OutputDir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}

		if err := os.WriteFile(outputPath, output, 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}

		k.Log.Infof("Successfully applied stage %s to %s", stage.DirName, outputPath)
	}

	return nil
}

// ApplyFinalStage applies the last stage in the pipeline (typical use case)
func (k *KustomizeApplier) ApplyFinalStage() error {
	// Discover all stages
	stages, err := internalTransform.DiscoverStages(k.TransformDir)
	if err != nil {
		return fmt.Errorf("failed to discover stages: %w", err)
	}

	if len(stages) == 0 {
		return fmt.Errorf("no stages found in transform directory")
	}

	// Get last stage
	lastStage := internalTransform.GetLastStage(stages)
	if lastStage == nil {
		return fmt.Errorf("failed to get last stage")
	}

	k.Log.Infof("Applying final stage: %s", lastStage.DirName)

	// Run kubectl kustomize build
	output, err := k.runKustomizeBuild(lastStage.Path)
	if err != nil {
		return fmt.Errorf("kubectl kustomize build failed: %w", err)
	}

	// Write to output.yaml (single file for final output)
	outputPath := filepath.Join(k.OutputDir, "output.yaml")
	if err := os.MkdirAll(k.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if err := os.WriteFile(outputPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	k.Log.Infof("Successfully applied final stage to %s", outputPath)
	return nil
}

// runKustomizeBuild executes kubectl kustomize build on a directory
func (k *KustomizeApplier) runKustomizeBuild(dir string) ([]byte, error) {
	cmd := exec.Command("kubectl", "kustomize", "build", dir)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	k.Log.Debugf("Running: kubectl kustomize build %s", dir)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("command failed: %w\nstderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// ValidateKubectlAvailable checks if kubectl command is available
func ValidateKubectlAvailable() error {
	cmd := exec.Command("kubectl", "version", "--client", "--short")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl not found or not executable: %w", err)
	}
	return nil
}
