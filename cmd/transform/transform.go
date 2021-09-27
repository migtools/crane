package transform

import (
	"context"
	//"errors"
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
)

type Options struct {
	globalFlags       *flags.GlobalFlags
	ExportDir         string
	PluginDir         string
	TransformDir      string
	IgnoredPatchesDir string
	PluginPriorities  string
	SkipPlugins       string
	OptionalFlags     string
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
		globalFlags: f,
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
	}

	cmd.AddCommand(optionals.NewOptionalsCommand(f))
	cmd.AddCommand(listplugins.NewListPluginsCommand(f))
	addFlagsForOptions(o, cmd)

	return cmd
}

func addFlagsForOptions(o *Options, cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.ExportDir, "export-dir", "e", "export", "The path where the kubernetes resources are saved")
	cmd.Flags().StringVarP(&o.PluginDir, "plugin-dir", "p", "plugins", "The path where binary plugins are located")
	cmd.Flags().StringVarP(&o.TransformDir, "transform-dir", "t", "transform", "The path where files that contain the transformations are saved")
	cmd.Flags().StringVar(&o.IgnoredPatchesDir, "ignored-patches-dir", "", "The path where files that contain transformations that were discarded due to conflicts are saved. If left blank, these files will not be saved.")
	cmd.Flags().StringVar(&o.PluginPriorities, "plugin-priorities", "", "A comma-separated list of plugin names. A plugin listed will take priority in the case of patch conflict over a plugin listed later in the list or over one not listed at all.")
	cmd.Flags().StringVarP(&o.SkipPlugins, "skip-plugins", "s", "", "A comma-separated list of plugins to skip")
	cmd.Flags().StringVar(&o.OptionalFlags, "optional-flags", "", "A semicolon-separated list of flag-name=value pairs. These flags with values will be passed into all plugins that are executed in the transform operation.")
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
		runner.OptionalFlags = o.getOptionalFlagsMap()
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
	for i, pluginName := range strings.Split(o.PluginPriorities, ",") {
		prioritiesMap[pluginName] = i
	}
	return prioritiesMap
}

func (o *Options) getOptionalFlagsMap() map[string]string {
	flagsMap := make(map[string]string)
	for _, flag := range strings.Split(o.OptionalFlags, ";") {
		if flag == "" {
			continue
		}
		flagKeyValue := strings.SplitN(flag, "=", 2)
		if len(flagKeyValue) == 0 || flagKeyValue[0] == "" {
			continue
		}
		if len(flagKeyValue) == 1 {
			flagsMap[flagKeyValue[0]] = ""
		} else {
			flagsMap[flagKeyValue[0]] = flagKeyValue[1]
		}
	}
	return flagsMap
}
