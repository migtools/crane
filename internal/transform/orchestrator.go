package transform

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
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
}

// RunSingleStage executes transform for a single stage (default mode)
func (o *Orchestrator) RunSingleStage(stageName, pluginName string) error {
	// Load all resources from export dir
	files, err := file.ReadFiles(context.TODO(), o.ExportDir)
	if err != nil {
		return fmt.Errorf("failed to read export directory: %w", err)
	}

	// Get all plugins
	allPlugins, err := plugin.GetFilteredPlugins(o.PluginDir, o.SkipPlugins, o.Log)
	if err != nil {
		return fmt.Errorf("failed to load plugins: %w", err)
	}

	// Filter plugins by the specified plugin name
	stage := Stage{PluginName: pluginName}
	plugins := o.filterPluginsByStage(allPlugins, stage)

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
			// Include stage name, resource identity, and type in error
			resourceID := fmt.Sprintf("%s/%s/%s", f.Unstructured.GetKind(), f.Unstructured.GetNamespace(), f.Unstructured.GetName())
			return fmt.Errorf("stage %s: failed to run transform for %s (type %T): %w", stageName, resourceID, f.Unstructured, err)
		}

		// Parse TransformFile (JSONPatch) to get patches
		var patches jsonpatch.Patch
		if len(response.TransformFile) > 2 && !response.HaveWhiteOut {
			patches, err = jsonpatch.DecodePatch(response.TransformFile)
			if err != nil {
				// Include stage name, resource identity, and type in error
				resourceID := fmt.Sprintf("%s/%s/%s", f.Unstructured.GetKind(), f.Unstructured.GetNamespace(), f.Unstructured.GetName())
				return fmt.Errorf("stage %s: failed to decode patches for %s (type %T): %w", stageName, resourceID, response, err)
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

		// Check if there's a previous stage in the full pipeline
		prevStage := GetPreviousStage(stages, stage)
		if prevStage == nil {
			// This is the first stage in the full pipeline, read from export directory
			files, err := file.ReadFiles(context.TODO(), o.ExportDir)
			if err != nil {
				return fmt.Errorf("failed to read export directory: %w", err)
			}
			for _, f := range files {
				inputResources = append(inputResources, f.Unstructured)
			}
		} else {
			// There's a previous stage, check if its output exists
			opts := file.PathOpts{
				TransformDir: o.TransformDir,
			}
			prevStageDir := opts.GetStageDir(prevStage.DirName)

			if _, err := os.Stat(prevStageDir); os.IsNotExist(err) {
				return fmt.Errorf("stage %s requires output from stage %s, but %s does not exist. "+
					"Run preceding stages in order, or use --stage %s for single-stage execution",
					stage.DirName, prevStage.DirName, prevStageDir, stage.DirName)
			}

			// Load from previous stage's output
			inputResources, err = o.loadStageOutput(*prevStage)
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

	writer := NewKustomizeWriter(opts, stage.DirName)
	if err := writer.WriteStage(artifacts, o.Force); err != nil {
		return err
	}

	return nil
}

// loadStageOutput loads resources from a completed stage's output
func (o *Orchestrator) loadStageOutput(stage Stage) ([]unstructured.Unstructured, error) {
	opts := file.PathOpts{
		TransformDir: o.TransformDir,
	}

	stageDir := opts.GetStageDir(stage.DirName)

	// Run kubectl kustomize to build the stage with patches applied
	cmd := exec.Command("kubectl", "kustomize", stageDir)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	o.Log.Debugf("Running: kubectl kustomize %s", stageDir)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to build stage %s: %w\nstderr: %s", stage.DirName, err, stderr.String())
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
			return nil, fmt.Errorf("failed to decode YAML document in stage %s: %w", stage.DirName, err)
		}

		// Skip empty documents
		if doc == nil {
			continue
		}

		// Convert the decoded document back to YAML bytes, then to JSON
		docBytes, err := yamlv3.Marshal(doc)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal YAML document in stage %s: %w", stage.DirName, err)
		}

		// Convert YAML to JSON
		jsonData, err := yaml.YAMLToJSON(docBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to convert YAML to JSON in stage %s: %w", stage.DirName, err)
		}

		// Unmarshal into unstructured
		u := unstructured.Unstructured{}
		if err := u.UnmarshalJSON(jsonData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal resource in stage %s: %w", stage.DirName, err)
		}

		resources = append(resources, u)
	}

	return resources, nil
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
