package add

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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
	log              *logrus.Logger
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
	o.globalFlags.SetCmdName("plugin-manager add")
	o.log = o.globalFlags.GetLoggerOrDefault()
	return nil
}

func (o *Options) Validate(args []string) error {
	// TODO: @jgabani
	log := o.log

	if len(args) != 1 {
		log.Warnf("Expected exactly one plugin name, got %d", len(args))
		return errors.New("please input only one plugin name")
	}

	if o.Global {
		if o.PluginDir == os.Getenv("HOME")+plugin.DefaultLocalPluginDir {
			o.PluginDir = plugin.GlobalPluginDir
		} else {
			log.Warnf("--plugin-dir and --global cannot be used together")
			return errors.New("--plugin-dir and --global should not be used together.")
		}
	}

	pluginDir, err := filepath.Abs(o.PluginDir)
	if err != nil {
		log.Errorf("Failed to resolve plugin directory path %q: %v", o.PluginDir, err)
		return err
	}

	files, err := ioutil.ReadDir(pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Debugf("Plugin directory %q does not exist, no installed plugins to check", pluginDir)
			return nil
		}
		log.Errorf("Failed to read plugin directory %q: %v", pluginDir, err)
		return err
	}

	paths, err := plugin.LocateBinaryInPluginDir(o.PluginDir, args[0], files)
	if err != nil {
		log.Errorf("Failed to locate plugin %s in %q: %v", args[0], o.PluginDir, err)
		return err
	}

	if len(paths) > 0 {
		// TODO: if a version is specified and the plugin is installed, have the discussion on what to do here
		for _, path := range paths {
			fmt.Printf("%s \n", path)
		}
		log.Warnf("Plugin %s is already installed", args[0])
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
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "installs the desired plugin",
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(c, args); err != nil {
				return err
			}
			if err := o.Validate(args); err != nil {
				o.log.Errorf("%s", err.Error())
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
	log := o.log

	log.Infof("Starting plugin-manager add %s...", args[0])

	manifestMap, err := plugin.BuildManifestMap(log, args[0], o.Repo)
	if err != nil {
		log.Errorf("Failed to build manifest for plugin %s: %v", args[0], err)
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
						uri, err := binaryURIForPlatform(value)
						if err != nil {
							log.Errorf("No binary available for plugin %s on this platform: %v", value.Name, err)
							return err
						}
						return downloadBinary(o.PluginDir, value.Name, uri, log)
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
						uri, err := binaryURIForPlatform(value)
						if err != nil {
							log.Errorf("No binary available for plugin %s %s on this platform: %v", value.Name, installVersion, err)
							return err
						}
						return downloadBinary(o.PluginDir, value.Name, uri, log)
					}
				}
				log.Errorf("The %s version of the plugin %s is not found", installVersion, args[0])
				fmt.Printf("Run \"crane plugin-manager list --name %s --params\" to see available versions along with additional information \n", args[0])
			default:
				// throw error saying that the plugin doest exists
				log.Errorf("The plugin %s is not found", args[0])
				fmt.Println("Run \"crane plugin-manager list\" to list all the available plugins")
			}
		}
	default:
		// throw error saying that the plugin doest exists
		log.Warnf("The plugin %s is not found", args[0])
		fmt.Println("Run \"crane plugin-manager list\" to list all the available plugins")
		return errors.New(fmt.Sprintf("The plugin %s is not found", args[0]))
	}
	return nil
}

func downloadBinary(pluginDir string, filename string, url string, log *logrus.Logger) error {
	if filepath.Base(filename) != filename || filename == "." || filename == ".." {
		return fmt.Errorf("invalid plugin filename %q", filename)
	}

	var binaryContents io.Reader
	isUrl, url := plugin.IsUrl(url)
	if !isUrl {
		srcPlugin, err := os.Open(url)
		if err != nil {
			log.Errorf("Failed to open local plugin binary %s: %v", url, err)
			return err
		}
		defer srcPlugin.Close()
		binaryContents = srcPlugin
	} else {
		// Get the data
		resp, err := http.Get(url)
		if err != nil {
			log.Errorf("Failed to download plugin binary from %s: %v", url, err)
			return err
		}
		defer resp.Body.Close()
		binaryContents = resp.Body
	}
	// Create dir if not exists
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		err = os.MkdirAll(pluginDir, os.ModePerm)
		if err != nil {
			log.Errorf("Failed to create plugin directory %s: %v", pluginDir, err)
			return err
		}
	}

	// Create the file
	pluginBinary, err := os.OpenFile(filepath.Join(pluginDir, filename), syscall.O_RDWR|syscall.O_CREAT|syscall.O_TRUNC, 0755)
	if err != nil {
		log.Errorf("Failed to create plugin file %s/%s: %v", pluginDir, filename, err)
		return err
	}
	defer pluginBinary.Close()

	// Write the body to filePluginDir
	_, err = io.Copy(pluginBinary, binaryContents)
	if err != nil {
		log.Errorf("Failed to write plugin binary %s: %v", filename, err)
		return err
	}
	err = pluginBinary.Sync()
	if err != nil {
		log.Errorf("Failed to sync plugin binary %s: %v", filename, err)
		return err
	}
	log.Infof("pluginBinary %s added to the path - %s", filename, pluginDir)
	return err
}

// binaryURIForPlatform returns the download URI for the current OS/arch.
// Returns an error if no matching binary is available.
func binaryURIForPlatform(version plugin.PluginVersion) (string, error) {
	for _, binary := range version.Binaries {
		if binary.OS == runtime.GOOS && binary.Arch == runtime.GOARCH {
			return binary.URI, nil
		}
	}
	return "", fmt.Errorf("plugin %s %s has no binary for %s/%s", version.Name, version.Version, runtime.GOOS, runtime.GOARCH)
}
