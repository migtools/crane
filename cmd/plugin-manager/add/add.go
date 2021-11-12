package add

import (
	"errors"
	"fmt"
	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/plugin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
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
	Version   string `mapstructure:"version"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// TODO: @jgabani
	return nil
}

func (o *Options) Validate(args []string) error {
	// TODO: @jgabani

	if len(args) != 1 {
		return errors.New("please input only one plugin name")
	}
	pluginDir, err := filepath.Abs(fmt.Sprintf("%v/%v", o.ManagedPluginDir(), o.Repo))
	if err != nil {
		return err
	}

	files, err := ioutil.ReadDir(pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	paths, err := plugin.LocateBinaryInPluginDir(o.ManagedPluginDir(), args[0], files)
	if err != nil {
		return err
	}

	if len(paths) > 0 {
		// TODO: if a version is specified and the plugin is installed, have the discussion on what to do here
		for _, path := range paths {
			fmt.Printf("%s \n", path)
		}
		return errors.New("the binary is already installed in the above path, either delete the binary or mention a repo from which the binary is needed")
	}
	return nil
}

func (o *Options) Run(args []string) error {
	return o.run(args)
}

func NewAddCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		globalFlags: f,
	}
	log := o.globalFlags.GetLogger()
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "installs the desired plugin",
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(c, args); err != nil {
				return err
			}
			if err := o.Validate(args); err != nil {
				log.Errorf(fmt.Sprintf("%s", err.Error()))
				return nil
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

	addFlagsForOptions(&o.cobraFlags, cmd)
	return cmd
}

func addFlagsForOptions(o *Flags, cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.Version, "version", "", "", "Install specific plugin version (if not passed, installs latest plugin version or the only available one)")
}

func (o *Options) run(args []string) error {
	log := o.globalFlags.GetLogger()

	manifestMap, err := plugin.BuildManifestMap(log, args[0], o.Repo)
	if err != nil {
		return nil
	}

	installVersion := "latest"
	if o.Version != "" {
		installVersion = o.Version
	}

	switch {
	case len(manifestMap) > 1:
		// if the plugin is found across multiple repository then fail and ask for a specific repo
		// TODO: if the version is mentioned look for a plugin with the same version, if found in only one repo add the same else fail and ask for the repo
		log.Errorf(fmt.Sprintf("The plugin %s is found across multiple repos, please specify one repo with --repo flag", args[0]))
	case len(manifestMap) == 1:
		// the plugin is found in only one repo
		for repo, manifest := range manifestMap {
			switch {
			// install the only available version of the plugin
			case len(manifest) == 1:
				for _, value := range manifest {
					// check if the version is mentioned and matches the version in manifest file
					if value.Name != "" && (o.Version == "" || string(value.Version) == o.Version) {
						return downloadBinary(fmt.Sprintf("%s/%s", o.ManagedPluginDir(), repo), value.Name, value.Binaries[0].URI, log)
					} else {
						log.Errorf(fmt.Sprintf("The version %s of plugin %s is not available", installVersion, value.Name))
						fmt.Printf("Run \"crane plugin-manager list --name %s --params\" to see available versions along with additional information \n", args[0])
					}
				}
			case len(manifest) > 1:
				// if there are multiple version of the plugins are available then look for the latest or mentioned version and if not found fail and ask user to input a version using --version flag
				for _, value := range manifest {
					if string(value.Version) == installVersion {
						return downloadBinary(fmt.Sprintf("%s/%s", o.ManagedPluginDir(), repo), value.Name, value.Binaries[0].URI, log)
					}
				}
				log.Errorf(fmt.Sprintf("The %s version of the plugin %s is not found", installVersion, args[0]))
				fmt.Printf("Run \"crane plugin-manager list --name %s --params\" to see available versions along with additional information \n", args[0])
			default:
				// throw error saying that the plugin doest exists
				log.Errorf(fmt.Sprintf("The plugin %s is not found", args[0]))
				fmt.Println(fmt.Sprintf("Run \"crane plugin-manager list\" to list all the available plugins \n"))
			}
		}
	default:
		// throw error saying that the plugin doest exists
		log.Errorf(fmt.Sprintf("The plugin %s is not found", args[0]))
		fmt.Println(fmt.Sprintf("Run \"crane plugin-manager list\" to list all the available plugins \n"))
	}
	return nil
}

func downloadBinary(filepath string, filename string, url string, log *logrus.Logger) error {
	var binaryContents io.Reader
	isUrl, url := plugin.IsUrl(url)
	if !isUrl {
		srcPlugin, err := os.Open(url)
		if err != nil {
			return err
		}
		defer srcPlugin.Close()
		binaryContents = srcPlugin
	} else {
		// Get the data
		resp, err := http.Get(url)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		binaryContents = resp.Body
	}
	// Create dir if not exists
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		err = os.MkdirAll(filepath, os.ModePerm)
		if err != nil {
			return err
		}
	}

	// Create the file
	plugin, err := os.OpenFile(filepath+"/"+filename, syscall.O_RDWR|syscall.O_CREAT|syscall.O_TRUNC, 0777)
	if err != nil {
		return err
	}
	defer plugin.Close()

	// Write the body to filePluginDir
	_, err = io.Copy(plugin, binaryContents)
	if err != nil {
		return err
	}
	err = plugin.Sync()
	if err != nil {
		return err
	}
	log.Infof("plugin %s added to the path - %s", filename, filepath)
	return err
}

func (o *Options) ManagedPluginDir() string {
	return fmt.Sprintf("%v/%v", o.PluginDir, plugin.MANAGED_DIR)
}
