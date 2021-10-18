package remove

import (
	"fmt"
	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/plugin"
	"github.com/spf13/cobra"
	"io/ioutil"
	"os"
	"path/filepath"
)

type Options struct {
	globalFlags *flags.GlobalFlags
	Repo        string
	PluginDir   string
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
		Use:   "remove",
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
	}
	addFlagsForOptions(o, cmd)
	return cmd
}

func addFlagsForOptions(o *Options, cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.Repo, "repo", "", "", "Remove plugin from specific repo (optional), if not passed iterate through all the repo and remove the desired plugin. In case of conflicting name the command fails and asks user to specify the repo from which to remove the plugin")
	cmd.Flags().StringVarP(&o.PluginDir, "plugin-dir", "p", "plugins/managed", "The path where binary plugins are located (default 'plugins/managed')")
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
	} else {
		err = os.Remove(paths[0])
		if err != nil {
			return err
		}
		log.Infof("The plugin %s removed from path - %s", args[0], paths[0])
	}

	return nil
}
