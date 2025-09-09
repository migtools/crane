package remove

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

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
	Repo      string `mapstructure:"repo"`
	PluginDir string `mapstructure:"plugin-dir"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// TODO: @jgabani
	return nil
}

func (o *Options) Validate() error {
	// TODO: @jgabani
	return nil
}

func (o *Options) Run(args []string) error {
	return o.run(args)
}

func NewRemoveCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		globalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "removes the desired plugin",
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(c, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			if err := o.Run(args); err != nil {
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

	return cmd
}

func (o *Options) run(args []string) error {
	log := o.globalFlags.GetLogger()
	pluginDir, err := filepath.Abs(fmt.Sprintf("%v/%v", o.PluginDir, o.Repo))
	if err != nil {
		return err
	}

	files, err := ioutil.ReadDir(pluginDir)
	if err != nil {
		return err
	}

	paths, err := plugin.LocateBinaryInPluginDir(pluginDir, args[0], files)
	if err != nil {
		return err
	}

	if len(paths) > 1 {
		// fail and ask for a specific repo
		log.Errorf("The binary is installed from multiple source, please specify repository from which you want to remove the plugin using --repo \n")
		fmt.Printf("The binary is present in the following path")
		for _, path := range paths {
			fmt.Printf("%s \n", path)
		}
	} else if len(paths) == 0 {
		log.Errorf("Plugin %s not found in the plugin dir %s", args[0], pluginDir)
		fmt.Printf("Run \"crane plugin-manager list --installed -p %s\" to see the list of installed plugins \n", pluginDir)
	} else {
		err = os.Remove(paths[0])
		if err != nil {
			return err
		}
		log.Infof("The plugin %s removed from path - %s", args[0], paths[0])
	}

	return nil
}
