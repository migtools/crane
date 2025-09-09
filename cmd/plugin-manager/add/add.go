package add

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"syscall"

	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/plugin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/mod/semver"
)

type Options struct {
	// Two GlobalFlags struct fields are needed
	// 1. cobraGlobalFlags for explicit CLI args parsed by cobra
	// 2. globalFlags for the args merged with values from the viper config fileno go-import meta tags ()

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
	Global    bool   `mapstructure:"global"`
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

	if o.Global {
		if o.PluginDir == os.Getenv("HOME")+plugin.DefaultLocalPluginDir {
			o.PluginDir = plugin.GlobalPluginDir
		} else {
			return errors.New("--plugin-dir and --global should not be used together.")
		}
	}

	pluginDir, err := filepath.Abs(o.PluginDir)
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

	paths, err := plugin.LocateBinaryInPluginDir(o.PluginDir, args[0], files)
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
				log.Errorf("%s", err.Error())
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
	cmd.Flags().BoolVar(&o.Global, "global", false, "Perform a global plugin install to /usr/local/share/crane/plugins")
}

func (o *Options) run(args []string) error {
	log := o.globalFlags.GetLogger()

	manifestMap, err := plugin.BuildManifestMap(log, args[0], o.Repo)
	if err != nil {
		return nil
	}

	installVersion := ""
	if o.Version != "" {
		installVersion = o.Version
	}

	switch {
	case len(manifestMap) > 1:
		// if the plugin is found across multiple repository then fail and ask for a specific repo
		// TODO: if the version is mentioned look for a plugin with the same version, if found in only one repo add the same else fail and ask for the repo
		log.Errorf("The plugin %s is found across multiple repos, please specify one repo with --repo flag", args[0])
	case len(manifestMap) == 1:
		// the plugin is found in only one repo
		for _, pluginsMap := range manifestMap {
			switch {
			// install the only available version of the plugin
			case len(pluginsMap[args[0]]) == 1:
				for _, value := range pluginsMap[args[0]] {
					// check if the version is mentioned and matches the version in pluginsMap file
					if value.Name != "" && (o.Version == "" || string(value.Version) == o.Version) {
						return downloadBinary(o.PluginDir, value.Name, value.Binaries[0].URI, log)
					} else {
						log.Errorf("The version %s of plugin %s is not available", installVersion, value.Name)
						fmt.Printf("Run \"crane plugin-manager list --name %s --params\" to see available versions along with additional information \n", args[0])
					}
				}
			case len(pluginsMap[args[0]]) > 1:
				// if there are multiple version of the plugins are available then look for the latest or mentioned version and if not found fail and ask user to input a version using --version flag
				if installVersion == "" {
					availableVersions := []string{}
					for _, value := range pluginsMap[args[0]] {
						availableVersions = append(availableVersions, string(value.Version))
					}
					semver.Sort(availableVersions)
					installVersion = availableVersions[len(availableVersions)-1]
				}
				for _, value := range pluginsMap[args[0]] {
					if string(value.Version) == installVersion {
						return downloadBinary(o.PluginDir, value.Name, value.Binaries[0].URI, log)
					}
				}
				log.Errorf("The %s version of the plugin %s is not found", installVersion, args[0])
				fmt.Printf("Run \"crane plugin-manager list --name %s --params\" to see available versions along with additional information \n", args[0])
			default:
				// throw error saying that the plugin doest exists
				log.Errorf("The plugin %s is not found", args[0])
				fmt.Println(fmt.Sprintf("Run \"crane plugin-manager list\" to list all the available plugins \n"))
			}
		}
	default:
		// throw error saying that the plugin doest exists
		fmt.Println(fmt.Sprintf("Run \"crane plugin-manager list\" to list all the available plugins \n"))
		return errors.New(fmt.Sprintf("The plugin %s is not found", args[0]))
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
	pluginBinary, err := os.OpenFile(filepath+"/"+filename, syscall.O_RDWR|syscall.O_CREAT|syscall.O_TRUNC, 0777)
	if err != nil {
		return err
	}
	defer pluginBinary.Close()

	// Write the body to filePluginDir
	_, err = io.Copy(pluginBinary, binaryContents)
	if err != nil {
		return err
	}
	err = pluginBinary.Sync()
	if err != nil {
		return err
	}
	log.Infof("pluginBinary %s added to the path - %s", filename, filepath)
	return err
}
