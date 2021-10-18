package plugin_manager

import (
	"github.com/konveyor/crane/cmd/plugin-manager/add"
	"github.com/konveyor/crane/cmd/plugin-manager/list"
	"github.com/konveyor/crane/cmd/plugin-manager/remove"
	"github.com/konveyor/crane/internal/flags"
	"github.com/spf13/cobra"
)

type Options struct {
	globalFlags *flags.GlobalFlags
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// TODO: @jgabani
	return nil
}

func (o *Options) Validate() error {
	// TODO: @jgabani
	return nil
}

func (o *Options) Run() error {
	return o.run()
}

func NewPluginManagerCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		globalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "plugin-manager",
		Short: "Plugin-manager is command that helps manage plugins",
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

	cmd.AddCommand(list.NewListCommand(f))
	cmd.AddCommand(add.NewAddCommand(f))
	cmd.AddCommand(remove.NewRemoveCommand(f))
	return cmd
}

func (o *Options) run() error {
	return nil
}
