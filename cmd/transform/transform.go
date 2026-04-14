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
	PluginPriorities  []string `mapstructure:"plugin-priorities"`
	SkipPlugins       []string `mapstructure:"skip-plugins"`
	OptionalFlags     string   `mapstructure:"optional-flags"`
	// Multi-stage flags
	Stage     string   `mapstructure:"stage"`
	FromStage string   `mapstructure:"from-stage"`
	ToStage   string   `mapstructure:"to-stage"`
	Stages    []string `mapstructure:"stages"`
	Force     bool     `mapstructure:"force"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	return nil
}

func (o *Options) Validate() error {
	// Validate mutually exclusive flags
	flagCount := 0
	if o.Stage != "" {
		flagCount++
	}
	if o.FromStage != "" || o.ToStage != "" {
		flagCount++
	}
	if len(o.Stages) > 0 {
		flagCount++
	}

	if flagCount > 1 {
		return fmt.Errorf("--stage, --from-stage/--to-stage, and --stages are mutually exclusive")
	}

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
	cmd.Flags().StringSliceVar(&o.PluginPriorities, "plugin-priorities", nil, "A comma-separated list of plugin names. A plugin listed will take priority in the case of patch conflict over a plugin listed later in the list or over one not listed at all.")
	cmd.Flags().StringVar(&o.OptionalFlags, "optional-flags", "", "JSON string holding flag value pairs to be passed to all plugins ran in transform operation. (ie. '{\"foo-flag\": \"foo-a=/data,foo-b=/data\", \"bar-flag\": \"bar-value\"}')")

	// Multi-stage flags
	cmd.Flags().StringVar(&o.Stage, "stage", "", "Run transform for a specific stage only (e.g., '10_KubernetesPlugin')")
	cmd.Flags().StringVar(&o.FromStage, "from-stage", "", "Run transform from this stage onwards (e.g., '20_OpenshiftPlugin')")
	cmd.Flags().StringVar(&o.ToStage, "to-stage", "", "Run transform up to and including this stage (e.g., '30_ImagestreamPlugin')")
	cmd.Flags().StringSliceVar(&o.Stages, "stages", nil, "Run transform for specific stages (comma-separated, e.g., '10_KubernetesPlugin,30_ImagestreamPlugin')")
	cmd.Flags().BoolVarP(&o.Force, "force", "f", false, "Force overwrite of existing stage directories even if they contain user modifications")

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

	// Parse plugin priorities
	var pluginPriorities map[string]int
	if len(o.PluginPriorities) > 0 {
		pluginPriorities = o.getPluginPrioritiesMap()
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
		Log:              log.WithField("command", "transform").Logger,
		ExportDir:        exportDir,
		TransformDir:     transformDir,
		PluginDir:        pluginDir,
		SkipPlugins:      o.SkipPlugins,
		PluginPriorities: pluginPriorities,
		OptionalFlags:    optionalFlags,
		Force:            o.Force,
		CraneVersion:     "v1.0.0", // TODO: Get from build version
	}

	// Determine which stages to run
	var selector internalTransform.StageSelector

	if o.Stage != "" || o.FromStage != "" || o.ToStage != "" || len(o.Stages) > 0 {
		// User specified which stages to run

		// Special handling for --stage: create it if it doesn't exist
		if o.Stage != "" {
			if err := o.ensureStageExists(orchestrator, o.Stage, transformDir, exportDir, log); err != nil {
				return fmt.Errorf("failed to ensure stage exists: %w", err)
			}
		}

		selector = internalTransform.StageSelector{
			Stage:     o.Stage,
			FromStage: o.FromStage,
			ToStage:   o.ToStage,
			Stages:    o.Stages,
		}
		log.Info("Running selected stages")
	} else {
		// No stage parameters given - discover existing stages or create default
		existingStages, err := internalTransform.DiscoverStages(transformDir)
		if err != nil {
			return fmt.Errorf("failed to discover stages: %w", err)
		}

		if len(existingStages) == 0 {
			// No stages exist - create default stage transform artifacts first
			log.Info("No existing stages found, creating default stage: 10_KubernetesPlugin")
			defaultStageName := "10_KubernetesPlugin"

			// Create stage artifacts (this writes kustomization.yaml, resources, patches)
			if err := orchestrator.RunSingleStage(defaultStageName, ""); err != nil {
				return fmt.Errorf("failed to create default stage: %w", err)
			}

			// Now run multi-stage on the newly created stage for sequential consistency
			selector = internalTransform.StageSelector{
				Stage: defaultStageName,
			}
			log.Info("Running newly created stage through multi-stage pipeline")
		} else {
			// Run all discovered stages
			log.Infof("Discovered %d existing stage(s), running all", len(existingStages))
			// Empty selector means run all stages
		}
	}

	return orchestrator.RunMultiStage(selector)
}

func (o *Options) getPluginPrioritiesMap() map[string]int {
	prioritiesMap := make(map[string]int)
	for i, pluginName := range o.PluginPriorities {
		if len(pluginName) > 0 {
			prioritiesMap[pluginName] = i
		}
	}
	return prioritiesMap
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

// ensureStageExists creates a stage if it doesn't exist
// If stage name ends with "Plugin", creates stage using that plugin
// Otherwise, creates empty pass-through stage for manual editing
func (o *Options) ensureStageExists(orchestrator *internalTransform.Orchestrator, stageName, transformDir, exportDir string, log *logrus.Logger) error {
	// Check if stage already exists
	stageDir := filepath.Join(transformDir, stageName)
	if _, err := os.Stat(stageDir); err == nil {
		// Stage exists, nothing to do
		return nil
	}

	log.Infof("Stage %s does not exist, creating it...", stageName)

	// Determine input directory (from previous stage or export)
	existingStages, err := internalTransform.DiscoverStages(transformDir)
	if err != nil {
		return fmt.Errorf("failed to discover existing stages: %w", err)
	}

	// Find the stage that should come before this one (by priority)
	var inputDir string
	if len(existingStages) == 0 {
		// No existing stages, use export dir
		inputDir = exportDir
		log.Debugf("No existing stages, using export directory as input: %s", inputDir)
	} else {
		// Use the last existing stage's output
		lastStage := internalTransform.GetLastStage(existingStages)
		opts := file.PathOpts{
			TransformDir: transformDir,
		}

		// Check if last stage has been run (has output)
		lastStageOutputDir := opts.GetStageOutputDir(lastStage.DirName)
		if _, err := os.Stat(lastStageOutputDir); os.IsNotExist(err) {
			// Output doesn't exist, we need to run the last stage first
			log.Infof("Previous stage %s hasn't been run yet, running it first...", lastStage.DirName)
			selector := internalTransform.StageSelector{Stage: lastStage.DirName}
			if err := orchestrator.RunMultiStage(selector); err != nil {
				return fmt.Errorf("failed to run previous stage %s: %w", lastStage.DirName, err)
			}
		}

		inputDir = lastStageOutputDir
		log.Debugf("Using previous stage output as input: %s", inputDir)
	}

	// Determine if this is a plugin stage or pass-through stage
	if strings.HasSuffix(stageName, "Plugin") {
		// Extract plugin name from stage name
		// Format: <priority>_<PluginName>
		parts := strings.SplitN(stageName, "_", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid stage name format: %s (expected <priority>_<PluginName>)", stageName)
		}
		pluginName := parts[1]

		log.Infof("Creating plugin-based stage %s using plugin: %s", stageName, pluginName)

		// Create stage using RunSingleStage (which handles plugin execution)
		if err := orchestrator.RunSingleStage(stageName, pluginName); err != nil {
			return fmt.Errorf("failed to create plugin stage: %w", err)
		}

		// Now run through multi-stage to generate .work/ directories
		selector := internalTransform.StageSelector{Stage: stageName}
		if err := orchestrator.RunMultiStage(selector); err != nil {
			return fmt.Errorf("failed to run newly created stage: %w", err)
		}
	} else {
		// Create pass-through stage for manual editing
		log.Infof("Creating pass-through stage %s (no plugin, ready for manual editing)", stageName)

		if err := orchestrator.CreatePassThroughStage(stageName, inputDir); err != nil {
			return fmt.Errorf("failed to create pass-through stage: %w", err)
		}

		// Run through multi-stage to generate .work/ directories
		selector := internalTransform.StageSelector{Stage: stageName}
		if err := orchestrator.RunMultiStage(selector); err != nil {
			return fmt.Errorf("failed to run newly created stage: %w", err)
		}
	}

	log.Infof("Successfully created stage: %s", stageName)
	return nil
}
