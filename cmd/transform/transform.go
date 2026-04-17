package transform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/cmd/transform/listplugins"
	"github.com/konveyor/crane/cmd/transform/optionals"
	"github.com/konveyor/crane/internal/file"
	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/plugin"
	internalTransform "github.com/konveyor/crane/internal/transform"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

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
}

type Flags struct {
	ExportDir         string   `mapstructure:"export-dir"`
	PluginDir         string   `mapstructure:"plugin-dir"`
	TransformDir      string   `mapstructure:"transform-dir"`
	IgnoredPatchesDir string   `mapstructure:"ignored-patches-dir"`
	SkipPlugins       []string `mapstructure:"skip-plugins"`
	OptionalFlags     string   `mapstructure:"optional-flags"`
	// Multi-stage flag
	Stage string `mapstructure:"stage"`
	Force bool   `mapstructure:"force"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
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
		Use:   "transform",
		Short: "Create the transformations for the exported resources and plugins and save the results in a transform directory",
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

	// Multi-stage flag
	cmd.Flags().StringVar(&o.Stage, "stage", "", "Run transform for a specific stage only (e.g., '10_KubernetesPlugin'). If not specified, all stages are run.")
	cmd.Flags().BoolVar(&o.Force, "force", false, "Force overwrite of existing stage directories even if they contain user modifications")

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

	// Parse optional flags
	var optionalFlags map[string]string
	if len(o.OptionalFlags) > 0 {
		err = json.Unmarshal([]byte(o.OptionalFlags), &optionalFlags)
		if err != nil {
			return err
		}
		optionalFlags = optionalFlagsToLower(optionalFlags)
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
	}

	// Determine which stages to run
	var selector internalTransform.StageSelector

	if o.Stage != "" {
		// User specified a specific stage to run
		// Ensure directory exists before running
		if err := o.ensureStageDirectoryExists(orchestrator, o.Stage, transformDir, exportDir, log); err != nil {
			return fmt.Errorf("failed to ensure stage directory exists: %w", err)
		}

		selector = internalTransform.StageSelector{
			Stage: o.Stage,
		}
		log.Infof("Running stage: %s", o.Stage)
	} else {
		// No stage parameters given - discover existing stages or create default
		existingStages, err := internalTransform.DiscoverStages(transformDir)
		if err != nil {
			return fmt.Errorf("failed to discover stages: %w", err)
		}

		if len(existingStages) == 0 {
			// No stages exist - will create default stage via multi-stage pipeline
			// We create an empty directory so DiscoverStages will find it, then
			// RunMultiStage → executeStage will populate it with artifacts
			log.Info("No existing stages found, creating default stage: 10_KubernetesPlugin")
			defaultStageName := "10_KubernetesPlugin"

			// Create empty stage directory
			stageDir := filepath.Join(transformDir, defaultStageName)
			if err := os.MkdirAll(stageDir, 0700); err != nil {
				return fmt.Errorf("failed to create default stage directory: %w", err)
			}

			// Mark this stage as newly created so executeStage can overwrite it
			orchestrator.NewlyCreatedStages[defaultStageName] = true

			// Run multi-stage on the default stage
			// executeStage will populate it with artifacts (kustomization, resources, patches)
			selector = internalTransform.StageSelector{
				Stage: defaultStageName,
			}
			log.Info("Populating and executing default stage")
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

// ensureStageDirectoryExists creates a stage directory if it doesn't exist
// and marks it as newly created so RunMultiStage can populate it
func (o *Options) ensureStageDirectoryExists(orchestrator *internalTransform.Orchestrator, stageName, transformDir, exportDir string, log *logrus.Logger) error {
	// Check if stage already exists
	stageDir := filepath.Join(transformDir, stageName)
	if _, err := os.Stat(stageDir); err == nil {
		// Stage directory exists, nothing to do
		log.Debugf("Stage %s already exists", stageName)
		return nil
	}

	log.Infof("Stage %s does not exist, creating directory...", stageName)

	// Before creating this stage, ensure previous stages have been run
	// (so we have input data available)
	existingStages, err := internalTransform.DiscoverStages(transformDir)
	if err != nil {
		return fmt.Errorf("failed to discover existing stages: %w", err)
	}

	// If there are existing stages, make sure the last one has been run
	if len(existingStages) > 0 {
		lastStage := internalTransform.GetLastStage(existingStages)
		opts := file.PathOpts{
			TransformDir: transformDir,
		}

		lastStageOutputDir := opts.GetStageOutputDir(lastStage.DirName)
		if _, err := os.Stat(lastStageOutputDir); os.IsNotExist(err) {
			// Previous stage hasn't been run yet, run it first
			log.Infof("Previous stage %s hasn't been run yet, running it first...", lastStage.DirName)

			// Run the stage - it will regenerate based on its type:
			// - Plugin stages: auto-regenerate (no --force needed)
			// - Custom stages: fail if directory not empty (require --force)
			selector := internalTransform.StageSelector{Stage: lastStage.DirName}
			if err := orchestrator.RunMultiStage(selector); err != nil {
				return fmt.Errorf("failed to run previous stage %s: %w", lastStage.DirName, err)
			}
		}
	}

	// Create empty stage directory
	if err := os.MkdirAll(stageDir, 0700); err != nil {
		return fmt.Errorf("failed to create stage directory: %w", err)
	}

	// Mark this stage as newly created
	// This allows RunMultiStage to populate it without --force
	orchestrator.NewlyCreatedStages[stageName] = true

	log.Infof("Created empty stage directory: %s (will be populated by multi-stage pipeline)", stageName)
	return nil
}
