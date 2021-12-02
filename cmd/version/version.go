package version

import (
	"fmt"

	"github.com/konveyor/crane/internal/buildinfo"
	"github.com/konveyor/crane/internal/flags"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type Options struct {
	// Two GlobalFlags struct fields are needed
	// 1. cobraGlobalFlags for explicit CLI args parsed by cobra
	// 2. globalFlags for the args merged with values from the viper config file
	cobraGlobalFlags *flags.GlobalFlags
	globalFlags      *flags.GlobalFlags
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

func NewVersionCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		cobraGlobalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Return the current crane (and crane-lib) version",
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
			viper.Unmarshal(&o.globalFlags)
		},
	}
	return cmd
}

func (o *Options) run() error {
	fmt.Println("crane:")
	fmt.Printf("\tVersion: %s\n", buildinfo.Version)
	fmt.Println("crane-lib:")
	fmt.Printf("\tVersion: %s\n", buildinfo.CranelibVersion)
	return nil
}
