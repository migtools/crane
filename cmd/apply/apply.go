package apply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	craneApply "github.com/konveyor/crane-lib/apply"
	"github.com/konveyor/crane/internal/apply"
	"github.com/konveyor/crane/internal/file"
	"github.com/konveyor/crane/internal/flags"
	internalTransform "github.com/konveyor/crane/internal/transform"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"sigs.k8s.io/yaml"
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
	ExportDir    string `mapstructure:"export-dir"`
	TransformDir string `mapstructure:"transform-dir"`
	OutputDir    string `mapstructure:"output-dir"`
	// Multi-stage flags
	Stage     string   `mapstructure:"stage"`
	FromStage string   `mapstructure:"from-stage"`
	ToStage   string   `mapstructure:"to-stage"`
	Stages    []string `mapstructure:"stages"`
	FinalOnly bool     `mapstructure:"final-only"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// Store command reference for flag checking
	o.cmd = c
	return nil
}

func (o *Options) Validate() error {
	// Validate mutually exclusive flags
	flagCount := 0
	if o.Flags.Stage != "" {
		flagCount++
	}
	if o.Flags.FromStage != "" || o.Flags.ToStage != "" {
		flagCount++
	}
	if len(o.Flags.Stages) > 0 {
		flagCount++
	}
	if o.cmd != nil && o.cmd.Flags().Changed("final-only") {
		flagCount++
	}

	if flagCount > 1 {
		return fmt.Errorf("--stage, --from-stage/--to-stage, --stages, and --final-only are mutually exclusive")
	}

	return nil
}

func (o *Options) Run() error {
	return o.run()
}

func NewApplyCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		cobraGlobalFlags: f,
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
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
			viper.Unmarshal(&o.Flags)
			viper.Unmarshal(&o.globalFlags)
		},
	}

	addFlagsForOptions(&o.cobraFlags, cmd)

	return cmd
}

func addFlagsForOptions(o *Flags, cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.ExportDir, "export-dir", "e", "export", "The path where the kubernetes resources are saved")
	cmd.Flags().StringVarP(&o.TransformDir, "transform-dir", "t", "transform", "The path where files that contain the transformations are saved")
	cmd.Flags().StringVarP(&o.OutputDir, "output-dir", "o", "output", "The path where files are to be saved after transformation are applied")

	// Multi-stage flags
	cmd.Flags().StringVar(&o.Stage, "stage", "", "Apply a specific stage only (e.g., '10_kubernetes')")
	cmd.Flags().StringVar(&o.FromStage, "from-stage", "", "Apply from this stage onwards (e.g., '20_openshift')")
	cmd.Flags().StringVar(&o.ToStage, "to-stage", "", "Apply up to and including this stage (e.g., '30_imagestream')")
	cmd.Flags().StringSliceVar(&o.Stages, "stages", nil, "Apply specific stages (comma-separated, e.g., '10_kubernetes,30_imagestream')")
	cmd.Flags().BoolVar(&o.FinalOnly, "final-only", true, "Apply only the final stage in the pipeline (default: true)")
}

func (o *Options) run() error {
	// Determine if using new Kustomize workflow or legacy apply
	useKustomize := o.shouldUseKustomizeWorkflow()

	if useKustomize {
		return o.runKustomizeWorkflow()
	}

	// Legacy apply workflow
	return o.runLegacyWorkflow()
}

// shouldUseKustomizeWorkflow determines if new Kustomize workflow should be used
func (o *Options) shouldUseKustomizeWorkflow() bool {
	// Use Kustomize workflow if any multi-stage flags are explicitly set by the user
	// OR if final-only is true (including default case when no flags are provided)
	return o.cmd.Flags().Changed("stage") || o.cmd.Flags().Changed("from-stage") ||
		o.cmd.Flags().Changed("to-stage") || o.cmd.Flags().Changed("stages") ||
		o.cmd.Flags().Changed("final-only") || o.FinalOnly
}

// runKustomizeWorkflow executes the new Kustomize-based apply
func (o *Options) runKustomizeWorkflow() error {
	log := o.globalFlags.GetLogger()

	transformDir, err := filepath.Abs(o.TransformDir)
	if err != nil {
		return err
	}

	outputDir, err := filepath.Abs(o.OutputDir)
	if err != nil {
		return err
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create applier
	applier := &apply.KustomizeApplier{
		Log:          log.WithField("command", "apply").Logger,
		TransformDir: transformDir,
		OutputDir:    outputDir,
	}

	// Validation removed for simplicity

	// Determine which stages to apply
	// If user specified which stages to run, use those
	if o.cmd.Flags().Changed("stage") || o.cmd.Flags().Changed("from-stage") ||
		o.cmd.Flags().Changed("to-stage") || o.cmd.Flags().Changed("stages") {
		// Multi-stage apply with selector
		selector := internalTransform.StageSelector{
			Stage:     o.Stage,
			FromStage: o.FromStage,
			ToStage:   o.ToStage,
			Stages:    o.Stages,
		}

		log.Info("Applying selected stages...")
		return applier.ApplyMultiStage(selector)
	}

	// Default or explicit --final-only: apply final stage
	log.Info("Applying final stage...")
	return applier.ApplyFinalStage()
}

// runLegacyWorkflow executes the old JSONPatch-based apply
func (o *Options) runLegacyWorkflow() error {
	log := o.globalFlags.GetLogger()
	a := craneApply.Applier{}

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
			log.Infof("resource file: %v is skipped due to white file: %v", f.Info.Name(), whPath)
			continue
		}

		// Set doc to the object, only update the file if the transfrom file exists
		doc, err := f.Unstructured.MarshalJSON()
		if err != nil {
			return err
		}

		tfPath := opts.GetTransformPath(f.Path)
		// Check if transform file exists
		// If the transform does not exist, assume that the resource file is
		// not needed and ignore for now.
		_, tfStatErr := os.Stat(tfPath)
		if tfStatErr != nil && !errors.Is(tfStatErr, os.ErrNotExist) {
			// Some other error here err out
			return tfStatErr
		}

		if !errors.Is(tfStatErr, os.ErrNotExist) {
			transformfile, err := os.ReadFile(tfPath)
			if err != nil {
				return err
			}

			doc, err = a.Apply(f.Unstructured, transformfile)
			if err != nil {
				return err
			}
		}

		y, err := yaml.JSONToYAML(doc)
		if err != nil {
			return err
		}
		outputFilePath := opts.GetOutputFilePath(f.Path)
		// We must create all the directories here.
		err = os.MkdirAll(filepath.Dir(outputFilePath), 0700)
		if err != nil {
			return err
		}
		outputFile, err := os.Create(outputFilePath)
		if err != nil {
			return err
		}
		defer outputFile.Close()
		i, err := outputFile.Write(y)
		if err != nil {
			return err
		}
		log.Debugf("wrote %v bytes for file: %v", i, outputFilePath)
	}

	return nil

}
