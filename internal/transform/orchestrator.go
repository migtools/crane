package transform

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	cranelib "github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane/internal/file"
	"github.com/konveyor/crane/internal/plugin"
	"github.com/sirupsen/logrus"
	yamlv3 "gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
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
	// NewlyCreatedStages tracks stages created in this run that can be overwritten
	// This prevents double-write errors when creating a stage and then running it
	NewlyCreatedStages map[string]bool
}

// RunMultiStage executes transform with multi-stage pipeline
// Each stage runs on the fully applied output of the previous stage
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
			// First stage reads from export directory
			inputDir = o.ExportDir
			o.Log.Debugf("Stage %s input: export directory (%s)", stage.DirName, inputDir)
		} else {
			// Subsequent stages read from previous stage's output
			prevStage := selectedStages[i-1]
			inputDir = opts.GetStageOutputDir(prevStage.DirName)
			o.Log.Debugf("Stage %s input: previous stage output (%s)", stage.DirName, inputDir)

			// Verify previous stage output exists
			if _, err := os.Stat(inputDir); os.IsNotExist(err) {
				return fmt.Errorf("stage %s requires output from stage %s, but output directory does not exist: %s",
					stage.DirName, prevStage.DirName, inputDir)
			}
		}

		// Step 2: Load input resources
		inputResources, err := o.loadResourcesFromDirectory(inputDir)
		if err != nil {
			return fmt.Errorf("stage %s: failed to load input resources from %s: %w", stage.DirName, inputDir, err)
		}

		o.Log.Infof("Stage %s: loaded %d input resource(s)", stage.DirName, len(inputResources))

		// Step 3: Save input to working directory for debugging
		stageInputDir := opts.GetStageInputDir(stage.DirName)
		if err := o.writeResourcesToDirectory(inputResources, stageInputDir); err != nil {
			return fmt.Errorf("stage %s: failed to write input snapshot: %w", stage.DirName, err)
		}

		// Step 4: Execute transform for this stage (generates transform artifacts)
		if err := o.executeStage(stage, inputResources); err != nil {
			return fmt.Errorf("stage %s: transform execution failed: %w", stage.DirName, err)
		}

		// Step 5: Apply transforms to get output resources
		stageTransformDir := opts.GetStageTransformDir(stage.DirName)
		outputResources, err := o.applyStageTransforms(stageTransformDir)
		if err != nil {
			return fmt.Errorf("stage %s: failed to apply transforms: %w", stage.DirName, err)
		}

		o.Log.Infof("Stage %s: produced %d output resource(s)", stage.DirName, len(outputResources))

		// Step 6: Write output to working directory (becomes input for next stage)
		stageOutputDir := opts.GetStageOutputDir(stage.DirName)
		if err := o.writeResourcesToDirectory(outputResources, stageOutputDir); err != nil {
			return fmt.Errorf("stage %s: failed to write output: %w", stage.DirName, err)
		}

		o.Log.Debugf("Stage %s: wrote output to %s", stage.DirName, stageOutputDir)
	}

	o.Log.Infof("Successfully completed %d stage(s)", len(selectedStages))
	return nil
}

// executeStage runs transform for a single stage
func (o *Orchestrator) executeStage(stage Stage, inputResources []unstructured.Unstructured) error {
	// Get all plugins
	allPlugins, err := plugin.GetFilteredPlugins(o.PluginDir, o.SkipPlugins, o.Log)
	if err != nil {
		return fmt.Errorf("failed to load plugins: %w", err)
	}

	// Filter plugins to only those matching this stage
	plugins := o.filterPluginsByStage(allPlugins, stage)

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
			// Include stage name, resource identity, and type in error
			resourceID := fmt.Sprintf("%s/%s/%s", resource.GetKind(), resource.GetNamespace(), resource.GetName())
			return fmt.Errorf("stage %s: failed to run transform for %s (type %T): %w", stage.DirName, resourceID, resource, err)
		}

		// Parse TransformFile (JSONPatch) to get patches
		var patches jsonpatch.Patch
		if len(response.TransformFile) > 2 && !response.HaveWhiteOut {
			patches, err = jsonpatch.DecodePatch(response.TransformFile)
			if err != nil {
				// Include stage name, resource identity, and type in error
				resourceID := fmt.Sprintf("%s/%s/%s", resource.GetKind(), resource.GetNamespace(), resource.GetName())
				return fmt.Errorf("stage %s: failed to decode patches for %s (type %T): %w", stage.DirName, resourceID, response, err)
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

	// Determine if we should force overwrite for this stage
	// Force is true if:
	// 1. Global Force flag is set, OR
	// 2. This stage was created in the current run (tracked in NewlyCreatedStages)
	forceWrite := o.Force
	if o.NewlyCreatedStages != nil && o.NewlyCreatedStages[stage.DirName] {
		forceWrite = true
		o.Log.Debugf("Stage %s was created in this run, allowing overwrite", stage.DirName)
	}

	writer := NewKustomizeWriter(opts, stage.DirName)
	if err := writer.WriteStage(artifacts, forceWrite); err != nil {
		return err
	}

	return nil
}

// filterPluginsByStage filters plugins to only those matching the stage's plugin name
func (o *Orchestrator) filterPluginsByStage(allPlugins []cranelib.Plugin, stage Stage) []cranelib.Plugin {
	// If stage has no specific plugin name, use all plugins
	if stage.PluginName == "" {
		return allPlugins
	}

	// Filter to only the plugin matching this stage
	var filtered []cranelib.Plugin
	for _, p := range allPlugins {
		if p.Metadata().Name == stage.PluginName {
			filtered = append(filtered, p)
		}
	}

	return filtered
}

// CreatePassThroughStage creates an empty pass-through stage that copies resources from input
// without applying any transformations. This is useful for manual editing stages.
func (o *Orchestrator) CreatePassThroughStage(stageName string, inputDir string) error {
	o.Log.Infof("Creating pass-through stage: %s", stageName)

	// Load input resources
	inputResources, err := o.loadResourcesFromDirectory(inputDir)
	if err != nil {
		return fmt.Errorf("failed to load input resources: %w", err)
	}

	// Create empty artifacts (no patches, no whiteouts)
	var artifacts []cranelib.TransformArtifact
	for _, resource := range inputResources {
		artifact := cranelib.TransformArtifact{
			Resource:     resource,
			HaveWhiteOut: false,
			Patches:      nil, // No patches
			IgnoredOps:   []cranelib.IgnoredOperation{},
			Target:       cranelib.DeriveTargetFromResource(resource),
			PluginName:   "", // No plugin
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
		return fmt.Errorf("failed to write pass-through stage: %w", err)
	}

	o.Log.Infof("Created pass-through stage %s with %d resources (ready for manual editing)", stageName, len(artifacts))
	return nil
}

// applyStageTransforms applies patches from a stage and returns the transformed resources
// This materializes the output by running kubectl kustomize or oc kustomize on the stage directory
func (o *Orchestrator) applyStageTransforms(stageDir string) ([]unstructured.Unstructured, error) {
	// Run kubectl kustomize or oc kustomize to build the stage with patches applied
	kustomizeCmd := file.GetKustomizeCommand()
	cmd := exec.Command(kustomizeCmd, "kustomize", stageDir)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	o.Log.Debugf("Running: %s kustomize %s", kustomizeCmd, stageDir)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s kustomize failed: %w\nstderr: %s", kustomizeCmd, err, stderr.String())
	}

	// Parse the multi-document YAML output
	var resources []unstructured.Unstructured

	// Use yaml.v3 Decoder to properly handle multi-document YAML streams
	decoder := yamlv3.NewDecoder(strings.NewReader(stdout.String()))

	for {
		var doc interface{}
		err := decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to decode YAML document: %w", err)
		}

		// Skip empty documents
		if doc == nil {
			continue
		}

		// Convert the decoded document back to YAML bytes, then to JSON
		docBytes, err := yamlv3.Marshal(doc)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal YAML document: %w", err)
		}

		// Convert YAML to JSON
		jsonData, err := yaml.YAMLToJSON(docBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to convert YAML to JSON: %w", err)
		}

		// Unmarshal into unstructured
		u := unstructured.Unstructured{}
		if err := u.UnmarshalJSON(jsonData); err != nil {
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
		return fmt.Errorf("failed to remove existing output directory: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0700); err != nil {
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
			return fmt.Errorf("failed to create resource directory: %w", err)
		}

		// Generate filename
		var filename string
		if namespace != "" {
			filename = fmt.Sprintf("%s_%s_%s.yaml", kind, namespace, name)
		} else {
			filename = fmt.Sprintf("%s_%s.yaml", kind, name)
		}

		filePath := filepath.Join(resourceDir, filename)

		// Marshal resource to YAML
		yamlBytes, err := yaml.Marshal(resource.Object)
		if err != nil {
			return fmt.Errorf("failed to marshal resource to YAML: %w", err)
		}

		if err := os.WriteFile(filePath, yamlBytes, 0644); err != nil {
			return fmt.Errorf("failed to write resource file: %w", err)
		}

		o.Log.Debugf("Wrote resource: %s", filePath)
	}

	return nil
}
