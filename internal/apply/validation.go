package apply

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/internal/file"
	internalTransform "github.com/konveyor/crane/internal/transform"
)

// ValidationResult contains validation results for a stage
type ValidationResult struct {
	StageName    string
	IsValid      bool
	Errors       []string
	Warnings     []string
	MetadataPath string
}

// ValidateStage performs preflight validation on a transform stage
func ValidateStage(transformDir, stageName string) (*ValidationResult, error) {
	result := &ValidationResult{
		StageName: stageName,
		IsValid:   true,
		Errors:    []string{},
		Warnings:  []string{},
	}

	opts := file.PathOpts{
		TransformDir: transformDir,
	}

	stageDir := opts.GetStageDir(stageName)

	// Check 1: Stage directory exists
	if _, err := os.Stat(stageDir); os.IsNotExist(err) {
		result.IsValid = false
		result.Errors = append(result.Errors, fmt.Sprintf("stage directory does not exist: %s", stageDir))
		return result, nil
	}

	// Check 2: kustomization.yaml exists
	kustomizationPath := opts.GetKustomizationPath(stageName)
	if _, err := os.Stat(kustomizationPath); os.IsNotExist(err) {
		result.IsValid = false
		result.Errors = append(result.Errors, "kustomization.yaml not found")
	}

	// Check 3: resources directory exists
	resourcesDir := opts.GetResourcesDir(stageName)
	if _, err := os.Stat(resourcesDir); os.IsNotExist(err) {
		result.IsValid = false
		result.Errors = append(result.Errors, "resources directory not found")
	} else {
		// Check if resources directory is empty
		entries, err := os.ReadDir(resourcesDir)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to read resources directory: %v", err))
		} else if len(entries) == 0 {
			result.Warnings = append(result.Warnings, "resources directory is empty")
		}
	}

	// Check 4: patches directory exists (optional, but check)
	patchesDir := opts.GetPatchesDir(stageName)
	if _, err := os.Stat(patchesDir); os.IsNotExist(err) {
		result.Warnings = append(result.Warnings, "patches directory not found (no patches will be applied)")
	}

	// Check 5: metadata exists
	metadataPath := opts.GetMetadataPath(stageName)
	result.MetadataPath = metadataPath
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		result.Warnings = append(result.Warnings, "metadata file not found (dirty check unavailable)")
	} else {
		// Check if stage has been modified
		dirty, err := internalTransform.IsDirectoryDirty(stageDir)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to check dirty status: %v", err))
		} else if dirty {
			result.Warnings = append(result.Warnings, "stage directory contains user modifications")
		}
	}

	// Check 6: Validate kustomization.yaml is well-formed
	if !result.IsValid {
		// Skip kustomization validation if basic checks failed
		return result, nil
	}

	// Try to parse kustomization.yaml by running kubectl kustomize
	if err := ValidateKubectlAvailable(); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("kubectl not available, skipping kustomization validation: %v", err))
	} else {
		applier := &KustomizeApplier{}
		_, err := applier.runKustomizeBuild(stageDir)
		if err != nil {
			result.IsValid = false
			result.Errors = append(result.Errors, fmt.Sprintf("kustomization.yaml validation failed: %v", err))
		}
	}

	return result, nil
}

// ValidateAllStages validates all stages in transform directory
func ValidateAllStages(transformDir string) ([]*ValidationResult, error) {
	// Discover stages
	stages, err := internalTransform.DiscoverStages(transformDir)
	if err != nil {
		return nil, fmt.Errorf("failed to discover stages: %w", err)
	}

	if len(stages) == 0 {
		return nil, fmt.Errorf("no stages found in transform directory")
	}

	// Validate each stage
	results := make([]*ValidationResult, 0, len(stages))
	for _, stage := range stages {
		result, err := ValidateStage(transformDir, stage.DirName)
		if err != nil {
			return nil, fmt.Errorf("failed to validate stage %s: %w", stage.DirName, err)
		}
		results = append(results, result)
	}

	return results, nil
}

// ValidatePipeline validates the entire multi-stage pipeline
func ValidatePipeline(transformDir string) error {
	results, err := ValidateAllStages(transformDir)
	if err != nil {
		return err
	}

	// Check if any stage has errors
	hasErrors := false
	for _, result := range results {
		if !result.IsValid {
			hasErrors = true
			fmt.Fprintf(os.Stderr, "Stage %s validation FAILED:\n", result.StageName)
			for _, err := range result.Errors {
				fmt.Fprintf(os.Stderr, "  ERROR: %s\n", err)
			}
		}

		// Print warnings
		for _, warning := range result.Warnings {
			fmt.Fprintf(os.Stderr, "  WARNING [%s]: %s\n", result.StageName, warning)
		}
	}

	if hasErrors {
		return fmt.Errorf("pipeline validation failed")
	}

	fmt.Printf("Pipeline validation passed (%d stages validated)\n", len(results))
	return nil
}

// ValidateOutputDirectory checks if output directory is suitable
func ValidateOutputDirectory(outputDir string, overwrite bool) error {
	// Check if directory exists
	stat, err := os.Stat(outputDir)
	if os.IsNotExist(err) {
		// Directory doesn't exist - that's fine, we'll create it
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to stat output directory: %w", err)
	}

	if !stat.IsDir() {
		return fmt.Errorf("output path exists but is not a directory: %s", outputDir)
	}

	// Check if directory is empty
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return fmt.Errorf("failed to read output directory: %w", err)
	}

	if len(entries) > 0 && !overwrite {
		return fmt.Errorf("output directory is not empty: %s (use --force to overwrite)", outputDir)
	}

	return nil
}

// ValidateStageChaining validates that stages can be chained correctly
func ValidateStageChaining(transformDir string) error {
	stages, err := internalTransform.DiscoverStages(transformDir)
	if err != nil {
		return fmt.Errorf("failed to discover stages: %w", err)
	}

	if len(stages) == 0 {
		return fmt.Errorf("no stages found")
	}

	// Validate each stage except the first one has a predecessor
	for i, stage := range stages {
		if i == 0 {
			// First stage - should have resources from export
			continue
		}

		// Check if previous stage exists and has output
		prevStage := stages[i-1]
		opts := file.PathOpts{
			TransformDir: transformDir,
		}
		prevResourcesDir := opts.GetResourcesDir(prevStage.DirName)

		if _, err := os.Stat(prevResourcesDir); os.IsNotExist(err) {
			return fmt.Errorf("stage %s depends on stage %s, but %s has no resources directory",
				stage.DirName, prevStage.DirName, prevStage.DirName)
		}

		// Check if previous stage has resources
		entries, err := os.ReadDir(prevResourcesDir)
		if err != nil {
			return fmt.Errorf("failed to read resources from stage %s: %w", prevStage.DirName, err)
		}

		if len(entries) == 0 {
			return fmt.Errorf("stage %s depends on stage %s, but %s has no resources",
				stage.DirName, prevStage.DirName, prevStage.DirName)
		}
	}

	return nil
}

// CheckResourceExists validates that a specific resource file exists
func CheckResourceExists(dir, filename string) bool {
	path := filepath.Join(dir, filename)
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
