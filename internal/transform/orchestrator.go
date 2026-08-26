package transform

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	cranelib "github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane/internal/file"
	"github.com/konveyor/crane/internal/kustomize"
	"github.com/konveyor/crane/internal/plugin"
	"github.com/sirupsen/logrus"
	yamlv3 "gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// StageArtifact wraps a transform artifact with crane-local routing metadata.
// IsNewResource tells the writer to place this artifact in the new/ directory
// instead of input/, without polluting the resource with temporary annotations.
type StageArtifact struct {
	cranelib.TransformArtifact
	IsNewResource bool
}

// Orchestrator coordinates multi-stage transform execution
type Orchestrator struct {
	Log                *logrus.Logger
	ExportDir          string
	TransformDir       string
	PluginDir          string
	SkipPlugins        []string
	OptionalFlags      map[string]string
	StageOptionalFlags map[string]map[string]string
	Overwrite          bool
	CraneVersion       string
	// NewlyCreatedStages tracks stages created in this run that can be overwritten
	// This prevents double-write errors when creating a stage and then running it
	NewlyCreatedStages map[string]bool
	// KustomizeArgs holds additional arguments for embedded kustomize (e.g. helm options)
	KustomizeArgs []string
	// StageKustomizeFragments holds per-stage inline kustomize fragments, keyed by
	// stage plugin/base name (e.g. "KubernetesPlugin"). Each fragment is merged
	// into the generated kustomization.yaml for that stage.
	StageKustomizeFragments map[string]map[string]interface{}
}

func (o *Orchestrator) validateStageOptionalFlags(stages []Stage) error {
	if len(o.StageOptionalFlags) == 0 {
		return nil
	}
	known := make(map[string]bool, len(stages))
	for _, s := range stages {
		known[s.PluginName] = true
	}
	for name := range o.StageOptionalFlags {
		if !known[name] {
			names := make([]string, len(stages))
			for i, s := range stages {
				names[i] = s.PluginName
			}
			o.Log.Debugf("Per-stage optionals reference unknown stage %q (known stages: %s)", name, strings.Join(names, ", "))
			return fmt.Errorf("per-stage optionals reference unknown stage %q (known stages: %s)", name, strings.Join(names, ", "))
		}
	}
	return nil
}

func (o *Orchestrator) validateStageKustomizeFragments(stages []Stage) error {
	if len(o.StageKustomizeFragments) == 0 {
		return nil
	}
	known := make(map[string]bool, len(stages))
	for _, s := range stages {
		known[s.PluginName] = true
	}
	for name := range o.StageKustomizeFragments {
		if !known[name] {
			names := make([]string, len(stages))
			for i, s := range stages {
				names[i] = s.PluginName
			}
			return fmt.Errorf("per-stage kustomize fragment references unknown stage %q (known stages: %s)", name, strings.Join(names, ", "))
		}
	}
	return nil
}

// resolveKustomizeFragment returns the inline kustomize fragment configured for
// the given stage, or nil if none is defined.
func (o *Orchestrator) resolveKustomizeFragment(stage Stage) map[string]interface{} {
	if o.StageKustomizeFragments == nil {
		return nil
	}
	return o.StageKustomizeFragments[stage.PluginName]
}

func (o *Orchestrator) resolveOptionalFlags(stage Stage) map[string]string {
	if o.StageOptionalFlags == nil {
		return o.OptionalFlags
	}
	stageFlags, ok := o.StageOptionalFlags[stage.PluginName]
	if !ok {
		return o.OptionalFlags
	}
	if len(o.OptionalFlags) == 0 {
		return stageFlags
	}
	merged := make(map[string]string, len(o.OptionalFlags)+len(stageFlags))
	for k, v := range o.OptionalFlags {
		merged[k] = v
	}
	for k, v := range stageFlags {
		merged[k] = v
	}
	return merged
}

// RunMultiStage executes transform with multi-stage pipeline
// Each stage runs on the fully applied output of the previous stage
func (o *Orchestrator) RunMultiStage(stageSelector StageSelector) error {
	// Load all plugins once at the start
	allPlugins, err := plugin.GetFilteredPlugins(o.PluginDir, o.SkipPlugins, o.Log)
	if err != nil {
		o.Log.Debugf("Failed to load plugins: %v", err)
		return fmt.Errorf("failed to load plugins: %w", err)
	}
	o.Log.Debugf("Loaded %d plugin(s)", len(allPlugins))

	// Discover all stages
	stages, err := DiscoverStages(o.TransformDir)
	if err != nil {
		o.Log.Debugf("Failed to discover stages in %s: %v", o.TransformDir, err)
		return fmt.Errorf("failed to discover stages: %w", err)
	}

	// Filter stages based on selector
	selectedStages := FilterStages(stages, stageSelector)

	if len(selectedStages) == 0 {
		o.Log.Debugf("No stages found matching selector in %s", o.TransformDir)
		return fmt.Errorf("no stages found matching selector")
	}

	if err := o.validateStageOptionalFlags(selectedStages); err != nil {
		o.Log.Errorf("Invalid stage optional flags: %v", err)
		return err
	}

	if err := o.validateStageKustomizeFragments(selectedStages); err != nil {
		return err
	}

	opts := file.PathOpts{
		TransformDir: o.TransformDir,
		ExportDir:    o.ExportDir,
	}

	// Execute each stage in order with sequential consistency
	for i, stage := range selectedStages {
		o.Log.Infof("Executing stage %d/%d: %s", i+1, len(selectedStages), stage.DirName)

		// Step 1: Determine input for this stage
		var inputDir string
		if i == 0 {
			// First selected stage - check if it's actually the first in the full pipeline
			// If not, use the previous stage's output instead of export
			prevStage := GetPreviousStage(stages, stage)
			if prevStage != nil {
				// This is not the first stage in the pipeline - use previous stage's output
				inputDir = opts.GetStageOutputDir(prevStage.DirName)
				o.Log.Debugf("Stage %s input: previous stage output (%s)", stage.DirName, inputDir)

				// Verify previous stage output exists
				if _, err := os.Stat(inputDir); os.IsNotExist(err) {
					o.Log.Debugf("Stage %s: output directory from stage %s does not exist: %s", stage.DirName, prevStage.DirName, inputDir)
					return fmt.Errorf("stage %s requires output from stage %s, but output directory does not exist: %s",
						stage.DirName, prevStage.DirName, inputDir)
				}
			} else {
				// This is the first stage in the pipeline - read from export directory
				inputDir = o.ExportDir
				o.Log.Debugf("Stage %s input: export directory (%s)", stage.DirName, inputDir)
			}
		} else {
			// Subsequent stages in selected set read from previous selected stage's output
			prevStage := selectedStages[i-1]
			inputDir = opts.GetStageOutputDir(prevStage.DirName)
			o.Log.Debugf("Stage %s input: previous stage output (%s)", stage.DirName, inputDir)

			// Verify previous stage output exists
			if _, err := os.Stat(inputDir); os.IsNotExist(err) {
				o.Log.Debugf("Stage %s: output directory from stage %s does not exist: %s", stage.DirName, prevStage.DirName, inputDir)
				return fmt.Errorf("stage %s requires output from stage %s, but output directory does not exist: %s",
					stage.DirName, prevStage.DirName, inputDir)
			}
		}

		// Step 2: Load input resources
		inputResources, err := o.loadResourcesFromDirectory(inputDir)
		if err != nil {
			o.Log.Debugf("Stage %s: failed to load input resources from %s: %v", stage.DirName, inputDir, err)
			return fmt.Errorf("stage %s: failed to load input resources from %s: %w", stage.DirName, inputDir, err)
		}

		o.Log.Infof("Stage %s: loaded %d input resource(s)", stage.DirName, len(inputResources))

		// Step 3: Execute transform for this stage (generates transform artifacts)
		// Note: Input resources will be written to input/ directory by executeStage via writer
		if err := o.executeStage(stage, inputResources, allPlugins); err != nil {
			o.Log.Debugf("Stage %s: transform execution failed: %v", stage.DirName, err)
			return fmt.Errorf("stage %s: transform execution failed: %w", stage.DirName, err)
		}

		// Step 4: Apply transforms to get output resources
		stageTransformDir := opts.GetStageTransformDir(stage.DirName)
		outputResources, err := o.applyStageTransforms(stageTransformDir)
		if err != nil {
			o.Log.Debugf("Stage %s: failed to apply transforms: %v", stage.DirName, err)
			return fmt.Errorf("stage %s: failed to apply transforms: %w", stage.DirName, err)
		}

		o.Log.Infof("Stage %s: produced %d output resource(s)", stage.DirName, len(outputResources))

		// Step 5: Write output (becomes input for next stage)
		stageOutputDir := opts.GetStageOutputDir(stage.DirName)
		if err := o.writeResourcesToDirectory(outputResources, stageOutputDir); err != nil {
			o.Log.Debugf("Stage %s: failed to write output: %v", stage.DirName, err)
			return fmt.Errorf("stage %s: failed to write output: %w", stage.DirName, err)
		}

		o.Log.Debugf("Stage %s: wrote output to %s", stage.DirName, stageOutputDir)
	}

	o.Log.Infof("Successfully completed %d stage(s)", len(selectedStages))
	return nil
}

// executeStage runs transform for a single stage
func (o *Orchestrator) executeStage(stage Stage, inputResources []unstructured.Unstructured, allPlugins []cranelib.Plugin) error {
	// Get the plugin for this stage (if any)
	stagePlugin, err := o.getPluginForStage(stage, allPlugins)
	if err != nil {
		o.Log.Errorf("Stage %s: failed to get plugin: %v", stage.DirName, err)
		return err
	}

	// Transform all resources through the plugin (or pass-through if no plugin)
	artifacts, err := o.transformResources(stage, stagePlugin, inputResources)
	if err != nil {
		o.Log.Errorf("Stage %s: failed to transform resources: %v", stage.DirName, err)
		return err
	}

	// Write stage output
	opts := file.PathOpts{
		TransformDir: o.TransformDir,
		ExportDir:    o.ExportDir,
	}

	// Determine write behavior based on stage type
	// Newly created stages: always allow overwrite (just created, safe to populate)
	// All other stages: respect --overwrite flag
	var forceWrite bool
	if o.NewlyCreatedStages != nil && o.NewlyCreatedStages[stage.DirName] {
		// Stage was just created in this run: safe to populate
		forceWrite = true
		o.Log.Debugf("Stage %s: allowing write (newly created in this run)", stage.DirName)
	} else {
		// All stages (plugin and custom): respect --overwrite flag
		forceWrite = o.Overwrite
		if forceWrite {
			o.Log.Debugf("Stage %s: allowing write (--overwrite flag set)", stage.DirName)
		} else {
			o.Log.Debugf("Stage %s: checking for empty directory (no --overwrite flag)", stage.DirName)
		}
	}

	writer := NewKustomizeWriter(opts, stage.DirName, o.Log)
	writer.kustomizeFragment = o.resolveKustomizeFragment(stage)
	if err := writer.WriteStage(artifacts, forceWrite); err != nil {
		o.Log.Errorf("Stage %s: failed to write stage: %v", stage.DirName, err)
		return err
	}

	return nil
}

// transformResources runs the plugin (if any) on all input resources
// Returns stage artifacts ready to be written to the stage directory
func (o *Orchestrator) transformResources(stage Stage, stagePlugin cranelib.Plugin, inputResources []unstructured.Unstructured) ([]StageArtifact, error) {
	// Build plugins list for runner (0 or 1 plugin)
	// Note: Runner.Run expects a slice, even though we only pass 0 or 1 plugin
	var plugins []cranelib.Plugin
	if stagePlugin != nil {
		plugins = []cranelib.Plugin{stagePlugin}
	}

	// Run transform
	// Note: PluginPriorities are not needed since each stage runs at most one plugin
	runner := cranelib.Runner{
		Log:              o.Log,
		PluginPriorities: nil, // No priorities needed - max 1 plugin per stage
		OptionalFlags:    o.resolveOptionalFlags(stage),
	}

	var artifacts []StageArtifact

	for _, resource := range inputResources {
		response, err := runner.Run(resource, plugins)
		if err != nil {
			resourceID := o.formatResourceID(resource)
			o.Log.Debugf("Stage %s: failed to run transform for %s: %v", stage.DirName, resourceID, err)
			return nil, fmt.Errorf("stage %s: failed to run transform for %s: %w", stage.DirName, resourceID, err)
		}

		// Parse TransformFile (JSONPatch) to get patches
		var patches jsonpatch.Patch
		if len(response.TransformFile) > 2 && !response.HaveWhiteOut {
			patches, err = jsonpatch.DecodePatch(response.TransformFile)
			if err != nil {
				resourceID := o.formatResourceID(resource)
				o.Log.Debugf("Stage %s: failed to decode patches for %s: %v", stage.DirName, resourceID, err)
				return nil, fmt.Errorf("stage %s: failed to decode patches for %s: %w", stage.DirName, resourceID, err)
			}
		}

		artifact := StageArtifact{
			TransformArtifact: cranelib.TransformArtifact{
				Resource:     resource,
				HaveWhiteOut: response.HaveWhiteOut,
				Patches:      patches,
				IgnoredOps:   []cranelib.IgnoredOperation{}, // TODO: Parse IgnoredPatches
				Target:       cranelib.DeriveTargetFromResource(resource),
				PluginName:   stage.PluginName,
			},
		}

		artifacts = append(artifacts, artifact)

		// Process new resources generated by the plugin
		for i, newResource := range response.NewResources {
			if newResource.GetKind() == "" {
				o.Log.Debugf("Stage %s: new resource #%d missing kind", stage.DirName, i)
				return nil, fmt.Errorf("stage %s: new resource #%d missing kind", stage.DirName, i)
			}
			if newResource.GetName() == "" {
				o.Log.Debugf("Stage %s: new resource #%d (%s) missing name", stage.DirName, i, newResource.GetKind())
				return nil, fmt.Errorf("stage %s: new resource #%d (%s) missing name", stage.DirName, i, newResource.GetKind())
			}
			if newResource.GetAPIVersion() == "" {
				o.Log.Debugf("Stage %s: new resource #%d (%s/%s) missing apiVersion", stage.DirName, i, newResource.GetKind(), newResource.GetName())
				return nil, fmt.Errorf("stage %s: new resource #%d (%s/%s) missing apiVersion", stage.DirName, i, newResource.GetKind(), newResource.GetName())
			}

			skeleton, newPatch, err := cranelib.SplitNewResourceToSkeletonAndPatch(newResource)
			if err != nil {
				o.Log.Debugf("Stage %s: failed to split new resource %s/%s: %v", stage.DirName, newResource.GetKind(), newResource.GetName(), err)
				return nil, fmt.Errorf("stage %s: failed to split new resource %s/%s: %w",
					stage.DirName, newResource.GetKind(), newResource.GetName(), err)
			}

			newArtifact := StageArtifact{
				TransformArtifact: cranelib.TransformArtifact{
					Resource:     skeleton,
					HaveWhiteOut: false,
					Patches:      newPatch,
					IgnoredOps:   []cranelib.IgnoredOperation{},
					Target:       cranelib.DeriveTargetFromResource(skeleton),
					PluginName:   stage.PluginName,
				},
				IsNewResource: true,
			}
			artifacts = append(artifacts, newArtifact)

			o.Log.Infof("Stage %s: plugin generated new resource: %s/%s/%s",
				stage.DirName, newResource.GetKind(), newResource.GetNamespace(), newResource.GetName())
		}
	}

	return artifacts, nil
}

// formatResourceID returns a human-readable identifier for a resource
func (o *Orchestrator) formatResourceID(resource unstructured.Unstructured) string {
	return fmt.Sprintf("%s/%s/%s", resource.GetKind(), resource.GetNamespace(), resource.GetName())
}

// getPluginForStage returns the plugin matching this stage from the provided plugin list
// Returns nil for pass-through stages (no plugin)
// Returns error if stage requires a plugin but it's not found
func (o *Orchestrator) getPluginForStage(stage Stage, allPlugins []cranelib.Plugin) (cranelib.Plugin, error) {
	// Find the plugin matching this stage's name
	var stagePlugin cranelib.Plugin
	for _, p := range allPlugins {
		if p.Metadata().Name == stage.PluginName {
			stagePlugin = p
			break
		}
	}

	// Validate: if stage name ends with "Plugin", the plugin must exist
	if strings.HasSuffix(stage.PluginName, "Plugin") && stagePlugin == nil {
		o.Log.Debugf("Stage %s: plugin %s not found (available: %s)", stage.DirName, stage.PluginName, o.getAvailablePluginNames(allPlugins))
		return nil, fmt.Errorf("stage %s requires plugin '%s' but it was not found (available plugins: %s). Stage names ending with 'Plugin' must have a corresponding plugin installed",
			stage.DirName, stage.PluginName, o.getAvailablePluginNames(allPlugins))
	}

	// Log which plugin (if any) will be used
	if stagePlugin != nil {
		o.Log.Debugf("Stage %s: using plugin %s", stage.DirName, stagePlugin.Metadata().Name)
	} else {
		o.Log.Debugf("Stage %s: pass-through (no plugin)", stage.DirName)
	}

	return stagePlugin, nil
}

// getAvailablePluginNames returns a comma-separated list of available plugin names
func (o *Orchestrator) getAvailablePluginNames(plugins []cranelib.Plugin) string {
	if len(plugins) == 0 {
		return "none"
	}
	names := make([]string, len(plugins))
	for i, p := range plugins {
		names[i] = p.Metadata().Name
	}
	return strings.Join(names, ", ")
}

// applyStageTransforms applies patches from a stage and returns the transformed resources
// This materializes the output by running embedded kustomize on the stage directory
func (o *Orchestrator) applyStageTransforms(stageDir string) ([]unstructured.Unstructured, error) {
	runner := &kustomize.Runner{
		Log:  o.Log,
		Args: o.KustomizeArgs,
	}

	output, err := runner.Build(stageDir)
	if err != nil {
		o.Log.Debugf("Kustomize build failed for stage directory %s: %v", stageDir, err)
		return nil, fmt.Errorf("kustomize build failed for stage directory %s: %w", stageDir, err)
	}

	// Check if kustomize produced any output
	// Empty output means the stage has no resources to transform
	if len(output) == 0 {
		o.Log.Debugf("Stage directory %s produced no resources to transform", stageDir)
		return nil, fmt.Errorf("stage produced no resources to transform.\n"+
			"This can happen when:\n"+
			"  - The stage received no input resources\n"+
			"  - The plugin processed resources but produced no transformations\n"+
			"  - A previous stage filtered out all resources")
	}

	// Parse the multi-document YAML output
	var resources []unstructured.Unstructured

	// Use yaml.v3 Decoder to properly handle multi-document YAML streams
	decoder := yamlv3.NewDecoder(strings.NewReader(string(output)))

	for {
		var doc interface{}
		err := decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			o.Log.Debugf("Failed to decode YAML document from stage %s: %v", stageDir, err)
			return nil, fmt.Errorf("failed to decode YAML document: %w", err)
		}

		// Skip empty documents
		if doc == nil {
			o.Log.Debugf("Skipping empty YAML document in stage %s", stageDir)
			continue
		}

		// Convert the decoded document back to YAML bytes, then to JSON
		docBytes, err := yamlv3.Marshal(doc)
		if err != nil {
			o.Log.Debugf("Failed to marshal YAML document from stage %s: %v", stageDir, err)
			return nil, fmt.Errorf("failed to marshal YAML document: %w", err)
		}

		// Convert YAML to JSON
		jsonData, err := yaml.YAMLToJSON(docBytes)
		if err != nil {
			o.Log.Debugf("Failed to convert YAML to JSON in stage %s: %v", stageDir, err)
			return nil, fmt.Errorf("failed to convert YAML to JSON: %w", err)
		}

		// Unmarshal into unstructured
		u := unstructured.Unstructured{}
		if err := u.UnmarshalJSON(jsonData); err != nil {
			o.Log.Debugf("Failed to unmarshal resource in stage %s: %v", stageDir, err)
			return nil, fmt.Errorf("failed to unmarshal resource: %w", err)
		}

		resources = append(resources, u)
	}

	return resources, nil
}

// loadResourcesFromDirectory loads all Kubernetes resources from a directory
func (o *Orchestrator) loadResourcesFromDirectory(dir string) ([]unstructured.Unstructured, error) {
	files, err := file.ReadFiles(context.TODO(), dir)
	if err != nil {
		o.Log.Debugf("Failed to read directory %s: %v", dir, err)
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	var resources []unstructured.Unstructured
	for _, f := range files {
		resources = append(resources, f.Unstructured)
	}

	return resources, nil
}

// writeResourcesToDirectory writes resources as individual YAML files to a directory
func (o *Orchestrator) writeResourcesToDirectory(resources []unstructured.Unstructured, outputDir string) error {
	// Clear output directory if it exists
	if err := os.RemoveAll(outputDir); err != nil && !os.IsNotExist(err) {
		o.Log.Debugf("Failed to remove existing output directory %s: %v", outputDir, err)
		return fmt.Errorf("failed to remove existing output directory: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0700); err != nil {
		o.Log.Debugf("Failed to create output directory %s: %v", outputDir, err)
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Write each resource to a separate file
	for _, resource := range resources {
		kind := resource.GetKind()
		namespace := resource.GetNamespace()
		name := resource.GetName()

		if kind == "" || name == "" {
			o.Log.Warnf("Skipping resource with missing kind or name")
			continue
		}

		// Determine subdirectory based on namespace
		var resourceDir string
		if namespace != "" {
			resourceDir = filepath.Join(outputDir, namespace)
		} else {
			resourceDir = filepath.Join(outputDir, "_cluster")
		}

		if err := os.MkdirAll(resourceDir, 0700); err != nil {
			o.Log.Debugf("Failed to create resource directory %s: %v", resourceDir, err)
			return fmt.Errorf("failed to create resource directory: %w", err)
		}

		// Generate filename using shared helper for consistency
		filename := file.GetResourceFilename(resource)
		filePath := filepath.Join(resourceDir, filename)

		// Marshal resource to YAML
		yamlBytes, err := yaml.Marshal(resource.Object)
		if err != nil {
			o.Log.Debugf("Failed to marshal resource %s to YAML: %v", filename, err)
			return fmt.Errorf("failed to marshal resource to YAML: %w", err)
		}

		if err := os.WriteFile(filePath, yamlBytes, 0644); err != nil {
			o.Log.Debugf("Failed to write resource file %s: %v", filePath, err)
			return fmt.Errorf("failed to write resource file: %w", err)
		}

		o.Log.Debugf("Wrote resource: %s", filePath)
	}

	return nil
}
