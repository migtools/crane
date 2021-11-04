package plugin_manager

import (
	"github.com/konveyor/crane/cmd/plugin-manager/add"
	"github.com/konveyor/crane/cmd/plugin-manager/list"
	"github.com/konveyor/crane/cmd/plugin-manager/remove"
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
	// Two Flags struct fields are needed
	// 1. cobraFlags for explicit CLI args parsed by cobra
	// 2. Flags for the args merged with values from the viper config file
	cobraFlags       Flags
	Flags
}

type Flags struct {
	PluginDir   string	`mapstructure:"plugin-dir"`
	Repo        string  `mapstructure:"repo"`
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
		cobraGlobalFlags: f,
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
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
			viper.Unmarshal(&o.globalFlags)
		},
	}
	addFlagsForOptions(&o.cobraFlags, cmd)
	cmd.AddCommand(list.NewListCommand(f))
	cmd.AddCommand(add.NewAddCommand(f))
	cmd.AddCommand(remove.NewRemoveCommand(f))
	return cmd
}
func addFlagsForOptions(o *Flags, cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&o.PluginDir, "plugin-dir", "p", "plugins", "The path where binary plugins are located")
	cmd.PersistentFlags().StringVarP(&o.Repo, "repo", "", "", "The name of the repository from which to list the plugins from")
}

func (o *Options) run() error {
	return nil
}
