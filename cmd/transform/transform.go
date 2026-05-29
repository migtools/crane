package transform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/konveyor/crane/cmd/transform/listplugins"
	"github.com/konveyor/crane/cmd/transform/optionals"
	"github.com/konveyor/crane/internal/file"
	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/kustomize"
	"github.com/konveyor/crane/internal/plugin"
	internalTransform "github.com/konveyor/crane/internal/transform"
	cranelib "github.com/konveyor/crane-lib/transform"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var safePluginNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type Options struct {
	// Two GlobalFlags struct fields are needed
	// 1. cobraGlobalFlags for explicit CLI args parsed by cobra
	// 2. globalFlags for the args merged with values from the viper config file
	cobraGlobalFlags *flags.GlobalFlags
	globalFlags      *flags.GlobalFlags
	// Two Flags struct fields are needed
	// 1. cobraFlags for explicit CLI args parsed by cobra
	// 2. Flags for the args merged with values from the viper config file
	cobraFlags Flags
	Flags
	// Positional arguments for stage selection
	RequestedStages []string
}

type Flags struct {
	ExportDir         string   `mapstructure:"export-dir"`
	PluginDir         string   `mapstructure:"plugin-dir"`
	TransformDir      string   `mapstructure:"transform-dir"`
	IgnoredPatchesDir string   `mapstructure:"ignored-patches-dir"`
	SkipPlugins       []string `mapstructure:"skip-plugins"`
	OptionalFlags     string   `mapstructure:"optional-flags"`
	Force             bool     `mapstructure:"force"`
	// Kustomize arguments
	KustomizeArgs string `mapstructure:"kustomize-args"`
	// Instructions file
	InstructionsFile string `mapstructure:"instructions-file"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// Store positional arguments as requested stages
	o.RequestedStages = args
	return nil
}

func (o *Options) Validate() error {
	// No validation needed - only --stage flag exists
	return nil
}

func (o *Options) Run() error {
	return o.run()
}

func NewTransformCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		cobraGlobalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "transform [stage...]",
		Short: "Create the transformations for the exported resources and plugins and save the results in a transform directory",
		Long: `Transform exported Kubernetes resources through one or more stages.

Stages can be specified by:
- Stage directory name (e.g., 10_KubernetesPlugin)
- Plugin name (e.g., KubernetesPlugin)

If no stages specified, all discovered stages are run.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(c, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			if err := o.Run(); err != nil {
				return err
			}

			return nil
		},
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
			viper.BindPFlags(cmd.PersistentFlags())
			viper.Unmarshal(&o.Flags)
			viper.Unmarshal(&o.globalFlags)
		},
	}

	addFlagsForOptions(&o.cobraFlags, cmd)
	cmd.AddCommand(optionals.NewOptionalsCommand(f))
	cmd.AddCommand(listplugins.NewListPluginsCommand(f))
	return cmd
}

func addFlagsForOptions(o *Flags, cmd *cobra.Command) {
	home := os.Getenv("HOME")
	defaultPluginDir := home + plugin.DefaultLocalPluginDir
	cmd.Flags().StringVarP(&o.ExportDir, "export-dir", "e", "export", "The path where the kubernetes resources are saved")
	cmd.Flags().StringVarP(&o.TransformDir, "transform-dir", "t", "transform", "The path where files that contain the transformations are saved")
	cmd.Flags().StringVar(&o.IgnoredPatchesDir, "ignored-patches-dir", "", "The path where files that contain transformations that were discarded due to conflicts are saved. If left blank, these files will not be saved.")
	cmd.Flags().StringVar(&o.OptionalFlags, "optional-flags", "", "JSON string holding flag value pairs to be passed to all plugins ran in transform operation. (ie. '{\"foo-flag\": \"foo-a=/data,foo-b=/data\", \"bar-flag\": \"bar-value\"}')")
	cmd.Flags().StringVar(&o.InstructionsFile, "instructions-file", "", "Path to the transform instructions file")
	cmd.Flags().BoolVar(&o.Force, "force", false, "Force overwrite of existing stage directories even if they contain user modifications")

	// Kustomize arguments
	cmd.Flags().StringVar(&o.KustomizeArgs, "kustomize-args", "", "Additional arguments for kustomize (e.g., '--enable-helm --helm-command=helm3')")

	// These flags pass down to subcommands
	cmd.PersistentFlags().StringVarP(&o.PluginDir, "plugin-dir", "p", defaultPluginDir, "The path where binary plugins are located")
	cmd.PersistentFlags().StringSliceVarP(&o.SkipPlugins, "skip-plugins", "s", nil, "A comma-separated list of plugins to skip")

}

func (o *Options) run() error {
	log := o.globalFlags.GetLogger()

	exportDir, err := filepath.Abs(o.ExportDir)
	if err != nil {
		return err
	}

	pluginDir, err := filepath.Abs(o.PluginDir)
	if err != nil {
		return err
	}

	transformDir, err := filepath.Abs(o.TransformDir)
	if err != nil {
		return err
	}

	if o.InstructionsFile != "" && len(o.RequestedStages) > 0 { // instructions file and positional args are mutually exclusive
		return fmt.Errorf("use either --instructions-file or positional stage arguments, not both")
	}
	var instructionStages []string
	if o.InstructionsFile != "" {
		instructionsFilePath, err := filepath.Abs(o.InstructionsFile)
		if err != nil {
			return fmt.Errorf("failed to resolve instructions file path %q: %w", o.InstructionsFile, err)
		}
		cfg, err := internalTransform.LoadInstructions(instructionsFilePath)
		if err != nil {
			return err
		}
		instructionStages = internalTransform.GenerateStageDirNames(cfg.Stages)
	}
	// Parse optional flags
	var optionalFlags map[string]string
	if len(o.OptionalFlags) > 0 {
		err = json.Unmarshal([]byte(o.OptionalFlags), &optionalFlags)
		if err != nil {
			return err
		}
		optionalFlags = optionalFlagsToLower(optionalFlags)
	}

	// Parse and validate kustomize arguments
	kustomizeArgs, err := kustomize.ParseAndValidateArgs(o.KustomizeArgs)
	if err != nil {
		return fmt.Errorf("invalid kustomize-args: %w", err)
	}

	// Create orchestrator
	orchestrator := &internalTransform.Orchestrator{
		Log:                log.WithField("command", "transform").Logger,
		ExportDir:          exportDir,
		TransformDir:       transformDir,
		PluginDir:          pluginDir,
		SkipPlugins:        o.SkipPlugins,
		OptionalFlags:      optionalFlags,
		Force:              o.Force,
		CraneVersion:       "v1.0.0", // TODO: Get from build version
		NewlyCreatedStages: make(map[string]bool),
		KustomizeArgs:      kustomizeArgs,
	}

	// Determine which stages to run
	var selector internalTransform.StageSelector
	if len(instructionStages) > 0 {
		log.Infof("Running stages from instructions file: %s", o.InstructionsFile)
		if err := o.reconcileInstructionStages(transformDir, instructionStages, log); err != nil {
			return err
		}
		for _, stageName := range instructionStages {
			stageDir := filepath.Join(transformDir, stageName)
			_, err := os.Stat(stageDir)
			stageExists := err == nil
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to inspect stage directory %s: %w", stageDir, err)
			}

			if !stageExists {
				if err := os.MkdirAll(stageDir, 0700); err != nil {
					return fmt.Errorf("failed to create stage directory %s: %w", stageName, err)
				}
				orchestrator.NewlyCreatedStages[stageName] = true
				log.Infof("Created stage directory: %s", stageDir)
			}
			selector = internalTransform.StageSelector{
				Stages: []string{stageName},
			}
			log.Infof("Running stage: %s", stageName)
			if err := o.runStageWithCleanup(orchestrator, selector, stageDir, !stageExists, log); err != nil {
				return err
			}
		}
		return nil
	}

	if len(o.RequestedStages) > 0 {
		// User specified specific stages to run via positional arguments
		resolvedStages, err := o.resolveAndValidateStages(o.RequestedStages, orchestrator, transformDir, pluginDir, log)
		if err != nil {
			return err
		}

		selector = internalTransform.StageSelector{
			Stages: resolvedStages,
		}
		log.Infof("Running %d stage(s): %v", len(resolvedStages), resolvedStages)

		return orchestrator.RunMultiStage(selector)
	} else {
		// No stage parameters given - discover existing stages or create default
		existingStages, err := internalTransform.DiscoverStages(transformDir)
		if err != nil {
			return fmt.Errorf("failed to discover stages: %w", err)
		}

		if len(existingStages) == 0 {
			// No stages exist - load all available plugins and create stages for each
			allPlugins, err := plugin.GetFilteredPlugins(pluginDir, o.SkipPlugins, log)
			if err != nil {
				return fmt.Errorf("failed to load plugins: %w", err)
			}

			if len(allPlugins) == 0 {
				return fmt.Errorf("no plugins found in plugin directories")
			}

			log.Infof("No existing stages found, creating default stages for %d plugin(s)", len(allPlugins))

			// Create stages for all plugins
			stageNames, err := o.createDefaultStagesForAllPlugins(orchestrator, transformDir, allPlugins, log)
			if err != nil {
				return fmt.Errorf("failed to create default stages: %w", err)
			}

			log.Infof("Created %d default stage(s): %v", len(stageNames), stageNames)

			// Empty selector = run all stages
			selector = internalTransform.StageSelector{}
			log.Info("Populating and executing all default stages")
		} else {
			// Run all discovered stages
			log.Infof("Discovered %d existing stage(s), running all", len(existingStages))
			// Empty selector means run all stages
		}
	}

	return orchestrator.RunMultiStage(selector)
}

// Returns an extras map with lowercased keys, since any keys coming from the config file
// are lower-cased by viper
func optionalFlagsToLower(inFlags map[string]string) map[string]string {
	lowerMap := make(map[string]string)
	for key, val := range inFlags {
		lowerMap[strings.ToLower(key)] = val
	}
	return lowerMap
}

// runStageWithCleanup runs a single stage and optionally cleans up on error.
// This is used by the instructions file code path which runs stages one at a time.
func (o *Options) runStageWithCleanup(orchestrator *internalTransform.Orchestrator, selector internalTransform.StageSelector, stageDir string, cleanupOnError bool, log *logrus.Logger) error {
	err := orchestrator.RunMultiStage(selector)
	if err != nil && cleanupOnError {
		log.Warnf("Stage execution failed, cleaning up stage directory: %s", stageDir)
		if removeErr := os.RemoveAll(stageDir); removeErr != nil {
			log.Errorf("Failed to clean up stage directory %s: %v", stageDir, removeErr)
		}
	}
	return err
}

// reconcileInstructionStages compares discovered stage directories in transform/
// against the desired stage names generated from --instructions-file.
// Without --force, it fails if extra stage directories are found.
// With --force, it deletes those extra stage directories so transform/
// matches the instructions-defined stage set.
func (o *Options) reconcileInstructionStages(transformDir string, desiredStages []string, log *logrus.Logger) error {
	existingStages, err := internalTransform.DiscoverStages(transformDir)
	if err != nil {
		return fmt.Errorf("failed to discover existing stages for instructions reconciliation: %w", err)
	}

	desiredSet := make(map[string]struct{}, len(desiredStages))
	for _, stage := range desiredStages {
		desiredSet[stage] = struct{}{}
	}

	var extras []string
	for _, stage := range existingStages {
		if _, exists := desiredSet[stage.DirName]; !exists {
			extras = append(extras, stage.DirName)
		}
	}

	if len(extras) == 0 {
		return nil
	}

	sort.Strings(extras)

	if !o.Force {
		return fmt.Errorf(
			"stages in transform/ do not match --instructions-file: extra stage directories: %s. Re-run with --force to reconcile",
			strings.Join(extras, ", "),
		)
	}

	for _, extra := range extras {
		stagePath := filepath.Join(transformDir, extra)              // transform/<stageName>
		stageWorkPath := filepath.Join(transformDir, ".work", extra) // transform/.work/<stageName>
		if err := os.RemoveAll(stagePath); err != nil {
			return fmt.Errorf("failed to delete extra stage directory %q at path %q: %w", extra, stagePath, err)
		}
		if err := os.RemoveAll(stageWorkPath); err != nil {
			return fmt.Errorf("failed to delete extra stage work directory %q at path %q: %w", extra, stageWorkPath, err)
		}
		log.Infof("Deleted stage directory not present in instructions file: %s", stagePath)
		log.Infof("Deleted stage work directory not present in instructions file: %s", stageWorkPath)
	}
	return nil
}


// ensurePreviousStagesRun ensures all existing stages have been run
// and have output directories. This prepares the environment for creating a new stage.
func (o *Options) ensurePreviousStagesRun(orchestrator *internalTransform.Orchestrator, transformDir string, log *logrus.Logger) error {
	// Discover all existing stages
	existingStages, err := internalTransform.DiscoverStages(transformDir)
	if err != nil {
		return fmt.Errorf("failed to discover existing stages: %w", err)
	}

	// Recursively ensure all existing stages have output
	if err := o.ensureStagesHaveOutput(orchestrator, existingStages, transformDir, log); err != nil {
		return err
	}

	return nil
}

// ensureStagesHaveOutput recursively ensures all stages in the list have been executed
// and have output directories. Stages are processed in order from first to last.
func (o *Options) ensureStagesHaveOutput(orchestrator *internalTransform.Orchestrator, stages []internalTransform.Stage, transformDir string, log *logrus.Logger) error {
	opts := file.PathOpts{
		TransformDir: transformDir,
	}

	for _, stage := range stages {
		stageOutputDir := opts.GetStageOutputDir(stage.DirName)

		// Check if this stage has output
		if _, err := os.Stat(stageOutputDir); os.IsNotExist(err) {
			// Stage hasn't been run yet, run it
			log.Infof("Stage %s hasn't been run yet, running it...", stage.DirName)

			// Run the stage - it will regenerate based on its type:
			// - Plugin stages: auto-regenerate (no --force needed)
			// - Custom stages: fail if directory not empty (require --force)
			selector := internalTransform.StageSelector{Stages: []string{stage.DirName}}
			if err := orchestrator.RunMultiStage(selector); err != nil {
				return fmt.Errorf("failed to run stage %s: %w", stage.DirName, err)
			}

			log.Infof("Stage %s completed successfully", stage.DirName)
		} else {
			log.Debugf("Stage %s already has output, skipping", stage.DirName)
		}
	}

	return nil
}

// createDefaultStagesForAllPlugins creates stage directories for all available plugins
// Returns list of stage names in priority order
func (o *Options) createDefaultStagesForAllPlugins(
	orchestrator *internalTransform.Orchestrator,
	transformDir string,
	allPlugins []cranelib.Plugin,
	log *logrus.Logger,
) ([]string, error) {
	var stageNames []string

	// Sort plugins by name for deterministic ordering
	sort.Slice(allPlugins, func(i, j int) bool {
		return allPlugins[i].Metadata().Name < allPlugins[j].Metadata().Name
	})

	// Assign priority to each plugin
	// Start at 10, increment by 5 for each plugin
	priority := 10

	for _, plugin := range allPlugins {
		pluginName := plugin.Metadata().Name

		// Validate plugin name is safe to use as directory name
		if err := validatePluginNameForStage(pluginName); err != nil {
			log.Warnf("Skipping plugin %q: %v", pluginName, err)
			continue
		}

		// Require "Plugin" suffix
		if !strings.HasSuffix(pluginName, "Plugin") {
			log.Warnf("Skipping plugin %q: name must end with 'Plugin'", pluginName)
			continue
		}

		// Use exact plugin metadata name in stage directory
		stageName := fmt.Sprintf("%d_%s", priority, pluginName)

		// Path traversal protection
		stageDir := filepath.Clean(filepath.Join(transformDir, stageName))
		cleanedTransformDir := filepath.Clean(transformDir)

		// Verify stageDir is within transformDir by computing relative path
		rel, err := filepath.Rel(cleanedTransformDir, stageDir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return nil, fmt.Errorf("invalid stage path generated for plugin %q: %q", pluginName, stageName)
		}

		log.Infof("Creating default stage for plugin: %s -> %s", pluginName, stageName)

		if err := os.MkdirAll(stageDir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create stage directory %s: %w", stageName, err)
		}

		// Mark as newly created
		orchestrator.NewlyCreatedStages[stageName] = true

		stageNames = append(stageNames, stageName)
		priority += 5
	}

	return stageNames, nil
}

// validatePluginNameForStage validates that a plugin name is safe to use as a stage directory name
// Returns error if the name is empty or contains unsafe characters
// Safe characters: A-Z, a-z, 0-9, hyphen, underscore
func validatePluginNameForStage(pluginName string) error {
	if pluginName == "" {
		return fmt.Errorf("plugin name is empty")
	}

	// Verify name only contains safe characters: alphanumeric, hyphen, underscore
	// This automatically rejects: /, \, .., ., and any special characters
	if !safePluginNameRE.MatchString(pluginName) {
		return fmt.Errorf("contains unsafe characters (only A-Z, a-z, 0-9, -, _ allowed)")
	}

	return nil
}

// resolveAndValidateStages resolves plugin names to stage directory names,
// validates stages exist or can be created, and handles mixed input
func (o *Options) resolveAndValidateStages(
	requestedStages []string,
	orchestrator *internalTransform.Orchestrator,
	transformDir string,
	pluginDir string,
	log *logrus.Logger,
) ([]string, error) {
	// Discover existing stages
	existingStages, err := internalTransform.DiscoverStages(transformDir)
	if err != nil {
		return nil, fmt.Errorf("failed to discover stages: %w", err)
	}

	var resolved []string
	seen := make(map[string]bool) // Prevent duplicates

	for _, requested := range requestedStages {
		// Skip if already resolved
		if seen[requested] {
			continue
		}

		// Check if it's an existing stage directory name
		found := false
		for _, stage := range existingStages {
			if stage.DirName == requested {
				resolved = append(resolved, requested)
				seen[requested] = true
				found = true
				break
			}
		}

		if found {
			continue
		}

		// Check if it's a plugin name - find matching stages
		var matchingStages []internalTransform.Stage
		for _, stage := range existingStages {
			if stage.PluginName == requested {
				matchingStages = append(matchingStages, stage)
			}
		}

		if len(matchingStages) > 1 {
			// ERROR: multiple stages with same plugin
			stageNames := make([]string, len(matchingStages))
			for i, s := range matchingStages {
				stageNames[i] = s.DirName
			}
			return nil, fmt.Errorf(
				"plugin %q found in multiple stages: %v. Please specify exact stage directory name",
				requested, stageNames)
		}

		if len(matchingStages) == 1 {
			// FOUND: use existing stage
			stageName := matchingStages[0].DirName
			if !seen[stageName] {
				log.Infof("Found existing stage for plugin %s: %s", requested, stageName)
				resolved = append(resolved, stageName)
				seen[stageName] = true
			}
			found = true
			continue
		}

		// Not found - try to create new stage for this plugin
		log.Infof("Stage for %q not found, attempting to create", requested)

		// Load plugins to verify it exists
		allPlugins, err := plugin.GetFilteredPlugins(pluginDir, o.SkipPlugins, log)
		if err != nil {
			return nil, fmt.Errorf("failed to load plugins: %w", err)
		}

		// Find matching plugin
		var matchedPlugin cranelib.Plugin
		for _, p := range allPlugins {
			if p.Metadata().Name == requested {
				matchedPlugin = p
				break
			}
		}

		if matchedPlugin == nil {
			return nil, fmt.Errorf("stage or plugin %q not found", requested)
		}

		// Ensure all previous stages have been run
		if err := o.ensurePreviousStagesRun(orchestrator, transformDir, log); err != nil {
			return nil, fmt.Errorf("failed to ensure previous stages are run: %w", err)
		}

		// Find next available priority
		maxPriority := 0
		for _, stage := range existingStages {
			if stage.Priority > maxPriority {
				maxPriority = stage.Priority
			}
		}
		newPriority := maxPriority + 10

		// Create stage for this plugin
		pluginName := matchedPlugin.Metadata().Name

		// Validate plugin name
		if err := validatePluginNameForStage(pluginName); err != nil {
			return nil, fmt.Errorf("invalid plugin name %q: %w", pluginName, err)
		}

		// Require Plugin suffix
		if !strings.HasSuffix(pluginName, "Plugin") {
			return nil, fmt.Errorf("plugin %q name must end with 'Plugin'", pluginName)
		}

		stageName := fmt.Sprintf("%d_%s", newPriority, pluginName)

		// Path traversal protection
		stageDir := filepath.Clean(filepath.Join(transformDir, stageName))
		cleanedTransformDir := filepath.Clean(transformDir)

		rel, err := filepath.Rel(cleanedTransformDir, stageDir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return nil, fmt.Errorf("invalid stage path generated for plugin %q: %q", pluginName, stageName)
		}

		log.Infof("Creating stage for plugin %s -> %s", pluginName, stageName)

		if err := os.MkdirAll(stageDir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create stage directory %s: %w", stageName, err)
		}

		// Mark as newly created for cleanup on error
		orchestrator.NewlyCreatedStages[stageName] = true

		resolved = append(resolved, stageName)
		seen[stageName] = true
	}

	if len(resolved) == 0 {
		return nil, fmt.Errorf("no valid stages found or could be created")
	}

	return resolved, nil
}
