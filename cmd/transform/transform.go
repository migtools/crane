package transform

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane/cmd/transform/listplugins"
	"github.com/konveyor/crane/cmd/transform/optionals"
	"github.com/konveyor/crane/internal/file"
	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/plugin"
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
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// TODO: @sseago
	return nil
}

func (o *Options) Validate() error {
	// TODO: @sseago
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
	// These flags pass down to subcommands
	cmd.PersistentFlags().StringVarP(&o.PluginDir, "plugin-dir", "p", defaultPluginDir, "The path where binary plugins are located")
	cmd.PersistentFlags().StringSliceVarP(&o.SkipPlugins, "skip-plugins", "s", nil, "A comma-separated list of plugins to skip")

}

func (o *Options) run() error {
	log := o.globalFlags.GetLogger()
	// Load all the resources from the export dir
	exportDir, err := filepath.Abs(o.ExportDir)
	if err != nil {
		// Handle errors better for users.
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

	var ignoredPatchesDir string
	if o.IgnoredPatchesDir != "" {
		ignoredPatchesDir, err = filepath.Abs(o.IgnoredPatchesDir)
		if err != nil {
			return err
		}
	}

	plugins, err := plugin.GetFilteredPlugins(pluginDir, o.SkipPlugins, log)
	if err != nil {
		return err
	}
	files, err := file.ReadFiles(context.TODO(), exportDir)
	if err != nil {
		return err
	}

	opts := file.PathOpts{
		TransformDir:      transformDir,
		ExportDir:         exportDir,
		IgnoredPatchesDir: ignoredPatchesDir,
	}

	runner := transform.Runner{Log: log.WithField("command", "transform").Logger}
	if len(o.PluginPriorities) > 0 {
		runner.PluginPriorities = o.getPluginPrioritiesMap()
	}

	if len(o.OptionalFlags) > 0 {
		err = json.Unmarshal([]byte(o.OptionalFlags), &runner.OptionalFlags)
		if err != nil {
			return err
		}
		runner.OptionalFlags = optionalFlagsToLower(runner.OptionalFlags)
		log.Debugf("parsed optional-flags: %v", runner.OptionalFlags)
	}

	for _, f := range files {
		response, err := runner.Run(f.Unstructured, plugins)
		if err != nil {
			return err
		}

		if response.HaveWhiteOut {
			whPath := opts.GetWhiteOutFilePath(f.Path)
			_, statErr := os.Stat(whPath)
			if os.IsNotExist(statErr) {
				log.Infof("resource file: %v creating whiteout file: %v", f.Info.Name(), whPath)
				err = os.MkdirAll(filepath.Dir(whPath), 0700)
				if err != nil {
					return err
				}
				whFile, err := os.Create(whPath)
				if err != nil {
					return err
				}
				whFile.Close()
			}
			continue
		} else {
			// if whiteout file exists from prior run, remove it
			whPath := opts.GetWhiteOutFilePath(f.Path)
			_, statErr := os.Stat(whPath)
			if !os.IsNotExist(statErr) {
				log.Infof("resource file: %v removing stale whiteout file: %v", f.Info.Name(), whPath)
				err := os.Remove(whPath)
				if err != nil {
					return err
				}
			}
		}

		// TODO: log if file exists and is truncated
		// TODO: delete transform file if it exists and haveWhiteOut
		tfPath := opts.GetTransformPath(f.Path)
		err = os.MkdirAll(filepath.Dir(tfPath), 0700)
		if err != nil {
			return err
		}
		transformFile, err := os.Create(tfPath)
		if err != nil {
			return err
		}
		defer transformFile.Close()
		i, err := transformFile.Write(response.TransformFile)
		if err != nil {
			return err
		}
		log.Debugf("wrote %v bytes for file: %v", i, tfPath)
		if len(response.IgnoredPatches) > 2 {
			log.Infof("Ignoring patches: %v", string(response.IgnoredPatches))
			if len(ignoredPatchesDir) > 0 {
				ignorePath := opts.GetIgnoredPatchesPath(f.Path)
				err = os.MkdirAll(filepath.Dir(ignorePath), 0700)
				if err != nil {
					return err
				}
				ignoreFile, err := os.Create(ignorePath)
				if err != nil {
					return err
				}
				defer ignoreFile.Close()
				i, err := ignoreFile.Write(response.IgnoredPatches)
				if err != nil {
					return err
				}
				log.Debugf("wrote %v bytes for file: %v", i, ignorePath)
			}
		}
	}
	return nil
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
