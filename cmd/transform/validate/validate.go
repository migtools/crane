package validate

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
	cobraGlobalFlags *flags.GlobalFlags
	globalFlags      *flags.GlobalFlags
	cobraFlags       Flags
	Flags
}

type Flags struct {
	TransformDir string `mapstructure:"transform-dir"`
	Verbose      bool   `mapstructure:"verbose"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	return nil
}

func (o *Options) Validate() error {
	if o.TransformDir == "" {
		return fmt.Errorf("transform-dir is required")
	}
	return nil
}

func (o *Options) Run() error {
	return o.run()
}

func NewValidateCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		cobraGlobalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate transform stage directories",
		Long: `Validate transform stage directories for correctness.

This command performs comprehensive validation of transform stages including:
- Stage directory structure
- kustomization.yaml syntax
- Resource file existence
- Patch file references
- Stage chaining correctness
- Dirty check status

Example usage:
  crane transform validate --transform-dir transform
  crane transform validate --transform-dir transform --verbose
`,
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
	cmd.Flags().StringVarP(&o.TransformDir, "transform-dir", "t", "transform", "The path where transform stages are located")
	cmd.Flags().BoolVarP(&o.Verbose, "verbose", "v", false, "Show verbose output including warnings")
}

func (o *Options) run() error {
	log := o.globalFlags.GetLogger()

	transformDir, err := filepath.Abs(o.Flags.TransformDir)
	if err != nil {
		return fmt.Errorf("failed to resolve transform directory: %w", err)
	}

	// Check if transform directory exists
	if _, err := os.Stat(transformDir); os.IsNotExist(err) {
		return fmt.Errorf("transform directory does not exist: %s", transformDir)
	}

	// Discover stages
	stages, err := internalTransform.DiscoverStages(transformDir)
	if err != nil {
		return fmt.Errorf("failed to discover stages: %w", err)
	}

	if len(stages) == 0 {
		fmt.Println("No stages found in transform directory")
		return nil
	}

	fmt.Printf("Found %d stage(s) in %s\n\n", len(stages), transformDir)

	// Validate each stage
	hasErrors := false
	hasWarnings := false

	for _, stage := range stages {
		fmt.Printf("Validating stage: %s (priority: %d, plugin: %s)\n", stage.DirName, stage.Priority, stage.PluginName)

		result, err := apply.ValidateStage(transformDir, stage.DirName)
		if err != nil {
			log.Errorf("Failed to validate stage %s: %v", stage.DirName, err)
			hasErrors = true
			continue
		}

		// Print errors
		if !result.IsValid {
			hasErrors = true
			for _, errMsg := range result.Errors {
				fmt.Fprintf(os.Stderr, "  ✗ ERROR: %s\n", errMsg)
			}
		}

		// Print warnings if verbose or if there are errors
		if o.Flags.Verbose || !result.IsValid {
			for _, warning := range result.Warnings {
				hasWarnings = true
				fmt.Fprintf(os.Stderr, "  ⚠ WARNING: %s\n", warning)
			}
		}

		// Print success if no errors
		if result.IsValid && len(result.Warnings) == 0 {
			fmt.Println("  ✓ Valid")
		} else if result.IsValid {
			fmt.Println("  ✓ Valid (with warnings)")
		}

		fmt.Println()
	}

	// Validate stage chaining
	fmt.Println("Validating stage chaining...")
	if err := apply.ValidateStageChaining(transformDir); err != nil {
		fmt.Fprintf(os.Stderr, "  ✗ ERROR: %v\n", err)
		hasErrors = true
	} else {
		fmt.Println("  ✓ Stage chaining is valid")
	}

	fmt.Println()

	// Summary
	if hasErrors {
		fmt.Fprintf(os.Stderr, "\n❌ Validation FAILED\n")
		return fmt.Errorf("validation failed with errors")
	}

	if hasWarnings {
		fmt.Printf("\n⚠️  Validation passed with warnings\n")
	} else {
		fmt.Printf("\n✅ Validation passed\n")
	}

	return nil
}
