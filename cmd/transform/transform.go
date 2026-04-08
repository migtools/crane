package transform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/cmd/transform/listplugins"
	"github.com/konveyor/crane/cmd/transform/optionals"
	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/plugin"
	internalTransform "github.com/konveyor/crane/internal/transform"
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
	// Command reference for checking flag changes
	cmd *cobra.Command
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
	Stage      string   `mapstructure:"stage"`
	FromStage  string   `mapstructure:"from-stage"`
	ToStage    string   `mapstructure:"to-stage"`
	Stages     []string `mapstructure:"stages"`
	StageName  string   `mapstructure:"stage-name"`
	PluginName string   `mapstructure:"plugin-name"`
	Force      bool     `mapstructure:"force"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// Store command reference for flag checking
	o.cmd = c
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
	cmd.Flags().StringVar(&o.Stage, "stage", "", "Run transform for a specific stage only (e.g., '10_kubernetes')")
	cmd.Flags().StringVar(&o.FromStage, "from-stage", "", "Run transform from this stage onwards (e.g., '20_openshift')")
	cmd.Flags().StringVar(&o.ToStage, "to-stage", "", "Run transform up to and including this stage (e.g., '30_imagestream')")
	cmd.Flags().StringSliceVar(&o.Stages, "stages", nil, "Run transform for specific stages (comma-separated, e.g., '10_kubernetes,30_imagestream')")
	cmd.Flags().StringVar(&o.StageName, "stage-name", "10_transform", "Name for the output stage directory (default: '10_transform')")
	cmd.Flags().StringVar(&o.PluginName, "plugin-name", "", "Plugin name to filter (empty = use all plugins)")
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

	// Check if multi-stage mode (user specified which stages to run)
	if o.cmd.Flags().Changed("stage") || o.cmd.Flags().Changed("from-stage") ||
		o.cmd.Flags().Changed("to-stage") || o.cmd.Flags().Changed("stages") {
		// Multi-stage mode
		selector := internalTransform.StageSelector{
			Stage:     o.Stage,
			FromStage: o.FromStage,
			ToStage:   o.ToStage,
			Stages:    o.Stages,
		}

		log.Info("Running multi-stage transform")
		return orchestrator.RunMultiStage(selector)
	}

	// Single stage mode (default)
	log.Infof("Running single-stage transform: %s", o.StageName)
	return orchestrator.RunSingleStage(o.StageName, o.PluginName)
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
