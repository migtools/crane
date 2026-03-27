package transform

import (
	"context"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	cranelib "github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane/internal/file"
	"github.com/konveyor/crane/internal/plugin"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Orchestrator coordinates multi-stage transform execution
type Orchestrator struct {
	Log              *logrus.Logger
	ExportDir        string
	TransformDir     string
	PluginDir        string
	SkipPlugins      []string
	PluginPriorities map[string]int
	OptionalFlags    map[string]string
	Force            bool
	CraneVersion     string
}

// RunSingleStage executes transform for a single stage (default mode)
func (o *Orchestrator) RunSingleStage(stageName, pluginName string) error {
	// Load all resources from export dir
	files, err := file.ReadFiles(context.TODO(), o.ExportDir)
	if err != nil {
		return fmt.Errorf("failed to read export directory: %w", err)
	}

	// Get filtered plugins
	plugins, err := plugin.GetFilteredPlugins(o.PluginDir, o.SkipPlugins, o.Log)
	if err != nil {
		return fmt.Errorf("failed to load plugins: %w", err)
	}

	// Run transform for each resource
	runner := cranelib.Runner{
		Log:              o.Log,
		PluginPriorities: o.PluginPriorities,
		OptionalFlags:    o.OptionalFlags,
	}

	var artifacts []cranelib.TransformArtifact

	for _, f := range files {
		response, err := runner.Run(f.Unstructured, plugins)
		if err != nil {
			return fmt.Errorf("failed to run transform for resource %s: %w", f.Info.Name(), err)
		}

		// Parse TransformFile (JSONPatch) to get patches
		var patches jsonpatch.Patch
		if len(response.TransformFile) > 2 && !response.HaveWhiteOut {
			patches, err = jsonpatch.DecodePatch(response.TransformFile)
			if err != nil {
				return fmt.Errorf("failed to decode patches for resource %s: %w", f.Info.Name(), err)
			}
		}

		// Convert runner response to TransformArtifact
		artifact := cranelib.TransformArtifact{
			Resource:     f.Unstructured,
			HaveWhiteOut: response.HaveWhiteOut,
			Patches:      patches,
			IgnoredOps:   []cranelib.IgnoredOperation{}, // TODO: Parse IgnoredPatches
			Target:       cranelib.DeriveTargetFromResource(f.Unstructured),
			PluginName:   pluginName,
		}

		artifacts = append(artifacts, artifact)
	}

	// Write stage output
	opts := file.PathOpts{
		TransformDir: o.TransformDir,
		ExportDir:    o.ExportDir,
	}

	writer := NewKustomizeWriter(opts, stageName)
	if err := writer.WriteStage(artifacts, o.Force); err != nil {
		return fmt.Errorf("failed to write stage output: %w", err)
	}

	o.Log.Infof("Successfully wrote transform stage: %s", stageName)
	return nil
}

// RunMultiStage executes transform with multi-stage pipeline
func (o *Orchestrator) RunMultiStage(stageSelector StageSelector) error {
	// Discover all stages
	stages, err := DiscoverStages(o.TransformDir)
	if err != nil {
		return fmt.Errorf("failed to discover stages: %w", err)
	}

	// Filter stages based on selector
	selectedStages := FilterStages(stages, stageSelector)

	if len(selectedStages) == 0 {
		return fmt.Errorf("no stages found matching selector")
	}

	// Execute each stage in order
	for i, stage := range selectedStages {
		o.Log.Infof("Executing stage %d/%d: %s", i+1, len(selectedStages), stage.DirName)

		// Load input resources
		var inputResources []unstructured.Unstructured
		if i == 0 {
			// First stage reads from export directory
			files, err := file.ReadFiles(context.TODO(), o.ExportDir)
			if err != nil {
				return fmt.Errorf("failed to read export directory: %w", err)
			}
			for _, f := range files {
				inputResources = append(inputResources, f.Unstructured)
			}
		} else {
			// Subsequent stages read from previous stage's output
			prevStage := selectedStages[i-1]
			inputResources, err = o.loadStageOutput(prevStage)
			if err != nil {
				return fmt.Errorf("failed to load output from stage %s: %w", prevStage.DirName, err)
			}
		}

		// Execute transform for this stage
		if err := o.executeStage(stage, inputResources); err != nil {
			return fmt.Errorf("failed to execute stage %s: %w", stage.DirName, err)
		}
	}

	o.Log.Infof("Successfully completed %d stage(s)", len(selectedStages))
	return nil
}

// executeStage runs transform for a single stage
func (o *Orchestrator) executeStage(stage Stage, inputResources []unstructured.Unstructured) error {
	// Get plugins for this stage (could be filtered by stage-specific config)
	plugins, err := plugin.GetFilteredPlugins(o.PluginDir, o.SkipPlugins, o.Log)
	if err != nil {
		return fmt.Errorf("failed to load plugins: %w", err)
	}

	// Run transform
	runner := cranelib.Runner{
		Log:              o.Log,
		PluginPriorities: o.PluginPriorities,
		OptionalFlags:    o.OptionalFlags,
	}

	var artifacts []cranelib.TransformArtifact

	for _, resource := range inputResources {
		response, err := runner.Run(resource, plugins)
		if err != nil {
			return fmt.Errorf("failed to run transform: %w", err)
		}

		// Parse TransformFile (JSONPatch) to get patches
		var patches jsonpatch.Patch
		if len(response.TransformFile) > 2 && !response.HaveWhiteOut {
			patches, err = jsonpatch.DecodePatch(response.TransformFile)
			if err != nil {
				return fmt.Errorf("failed to decode patches: %w", err)
			}
		}

		artifact := cranelib.TransformArtifact{
			Resource:     resource,
			HaveWhiteOut: response.HaveWhiteOut,
			Patches:      patches,
			IgnoredOps:   []cranelib.IgnoredOperation{}, // TODO: Parse IgnoredPatches
			Target:       cranelib.DeriveTargetFromResource(resource),
			PluginName:   stage.PluginName,
		}

		artifacts = append(artifacts, artifact)
	}

	// Write stage output
	opts := file.PathOpts{
		TransformDir: o.TransformDir,
		ExportDir:    o.ExportDir,
	}

	writer := NewKustomizeWriter(opts, stage.DirName)
	if err := writer.WriteStage(artifacts, o.Force); err != nil {
		return err
	}

	return nil
}

// loadStageOutput loads resources from a completed stage's output
func (o *Orchestrator) loadStageOutput(stage Stage) ([]unstructured.Unstructured, error) {
	// Read all resource files from stage's resources directory
	opts := file.PathOpts{
		TransformDir: o.TransformDir,
	}

	resourcesDir := opts.GetResourcesDir(stage.DirName)

	// Use kubectl kustomize to build the final output
	// For now, just read the resource files directly
	files, err := file.ReadFiles(context.TODO(), resourcesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read stage resources: %w", err)
	}

	var resources []unstructured.Unstructured
	for _, f := range files {
		resources = append(resources, f.Unstructured)
	}

	return resources, nil
}
