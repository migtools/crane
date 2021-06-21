package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/konveyor/crane-lib/apply"
	"github.com/konveyor/crane/internal/file"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

type Options struct {
	logger       logrus.FieldLogger
	ExportDir    string
	TransformDir string
	OutputDir    string
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// TODO: @shawn-hurley
	return nil
}

func (o *Options) Validate() error {
	// TODO: @shawn-hurley
	return nil
}

func (o *Options) Run() error {
	return o.run()
}

func NewApplyCommand() *cobra.Command {
	o := &Options{
		logger: logrus.New(),
	}
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the transformations to the exported resources and save results in an output directory",
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
	cmd.Flags().StringVarP(&o.TransformDir, "transform-dir", "t", "transform", "The path where files that contain the transformations are saved")
	cmd.Flags().StringVarP(&o.OutputDir, "output-dir", "o", "output", "The path where files are to be saved after transformation are applied")
}

func (o *Options) run() error {
	// log := o.logger
	a := apply.Applier{}

	// Load all the resources from the export dir
	exportDir, err := filepath.Abs(o.ExportDir)
	if err != nil {
		// Handle errors better for users.
		return err
	}

	transformDir, err := filepath.Abs(o.TransformDir)
	if err != nil {
		return err
	}

	outputDir, err := filepath.Abs(o.OutputDir)
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
		OutputDir:    outputDir,
	}

	//TODO: @shawn-hurley handle case where transform or whiteout file is not present.
	for _, f := range files {
		whPath := opts.GetWhiteOutFilePath(f.Path)
		_, statErr := os.Stat(whPath)
		if !errors.Is(statErr, os.ErrNotExist) {
			o.logger.Infof("resource file: %v is skipped due to white file: %v", f.Info.Name(), whPath)
			continue
		}

		tfPath := opts.GetTransformPath(f.Path)
		// Check if transform file exists
		// If the transform does not exist, assume that the resource file is
		// not needed and ignore for now.
		_, tfStatErr := os.Stat(whPath)
		if errors.Is(tfStatErr, os.ErrNotExist) {
			o.logger.Infof("resource file: %v is skipped due to no transform file: %v", f.Info.Name(), whPath)
			continue
		}

		transformfile, err := os.ReadFile(tfPath)
		if err != nil {
			return err
		}

		doc, err := a.Apply(f.Unstructured, transformfile)
		if err != nil {
			return err
		}

		y, err := yaml.JSONToYAML(doc)
		if err != nil {
			return err
		}
		outputFilePath := opts.GetOutputFilePath(f.Path)
		outputFile, err := os.Create(outputFilePath)
		if err != nil {
			return err
		}
		defer outputFile.Close()
		i, err := outputFile.Write(y)
		if err != nil {
			return err
		}
		o.logger.Debugf("wrote %v bytes for file: %v", i, outputFilePath)
	}

	return nil

}
