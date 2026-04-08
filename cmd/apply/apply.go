package apply

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/internal/apply"
	"github.com/konveyor/crane/internal/flags"
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
	ExportDir    string `mapstructure:"export-dir"`
	TransformDir string `mapstructure:"transform-dir"`
	OutputDir    string `mapstructure:"output-dir"`
	// Multi-stage flags
	Stage     string   `mapstructure:"stage"`
	FromStage string   `mapstructure:"from-stage"`
	ToStage   string   `mapstructure:"to-stage"`
	Stages    []string `mapstructure:"stages"`
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

	if flagCount > 1 {
		return fmt.Errorf("--stage, --from-stage/--to-stage, and --stages are mutually exclusive")
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
}

func (o *Options) run() error {
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

	// Default: apply final stage only
	log.Info("Applying final stage...")
	return applier.ApplyFinalStage()
}
