package transform

import (
	"context"
	//"errors"
	"os"
	"path/filepath"

	"github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane/internal/file"
	"github.com/konveyor/crane/internal/plugin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type Options struct {
	logger       logrus.FieldLogger
	ExportDir    string
	PluginDir    string
	TransformDir string
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

func NewTransformCommand() *cobra.Command {
	o := &Options{
		logger: logrus.New(),
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

	addFlagsForOptions(o, cmd)

	return cmd
}

func addFlagsForOptions(o *Options, cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.ExportDir, "export-dir", "e", "export", "The path where the kubernetes resources are saved")
	cmd.Flags().StringVarP(&o.PluginDir, "plugin-dir", "p", "plugins", "The path where binary plugins are located")
	cmd.Flags().StringVarP(&o.TransformDir, "transform-dir", "t", "transform", "The path where files that contain the transformations are saved")
}

func (o *Options) run() error {
	runner := transform.Runner{}

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

	plugins, err := plugin.GetBinaryPlugins(pluginDir)
	if err != nil {
		return err
	}

	files, err := file.ReadFiles(context.TODO(), exportDir)
	if err != nil {
		return err
	}

	opts := file.PathOpts{
		TransformDir: transformDir,
		ExportDir:    exportDir,
	}

	runner = transform.Runner{}

	for _, f := range files {
		response, err := runner.Run(f.Unstructured, plugins)
		if err != nil {
			return err
		}

		if response.HaveWhiteOut {
			whPath := opts.GetWhiteOutFilePath(f.Path)
			_, statErr := os.Stat(whPath)
			if os.IsNotExist(statErr) {
				o.logger.Infof("resource file: %v creating whiteout file: %v", f.Info.Name(), whPath)
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
				o.logger.Infof("resource file: %v removing stale whiteout file: %v", f.Info.Name(), whPath)
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
		o.logger.Debugf("wrote %v bytes for file: %v", i, tfPath)
		if len(response.IgnoredPatches) > 2 {
			o.logger.Infof("Ignoring patches: %v", string(response.IgnoredPatches))
		}
	}
	return nil
}
