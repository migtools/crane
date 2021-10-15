package add

import (
	"errors"
	"fmt"
	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/plugin"
	"github.com/spf13/cobra"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
)

type Options struct {
	globalFlags *flags.GlobalFlags
	Repo        string
	PluginDir   string
	Version     string
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// TODO: @jgabani
	return nil
}

func (o *Options) Validate(args []string) error {
	// TODO: @jgabani
	log := o.globalFlags.GetLogger()

	pluginDir, err := filepath.Abs(fmt.Sprintf("%v/%v", o.PluginDir, o.Repo))
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
		log.Errorf("The binary is already installed in the following path, either delete the binary or mention a repo from which the binary is needed")
		for _, path := range paths {
			fmt.Printf("%s \n", path)
		}
		return errors.New("the binary is already installed in the following path, either delete the binary or mention a repo from which the binary is needed")
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
		Use:   "add",
		Short: "installs the desired plugin",
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(c, args); err != nil {
				return err
			}
			if err := o.Validate(args); err != nil {
				return nil
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
	cmd.Flags().StringVarP(&o.Repo, "repo", "", "", "Install plugin from specific repo (optional), if not passed iterate through all the repo and install the desired plugin. In case of conflicting name the command fails and asks user to specify the repo from which to install the plugin")
	cmd.Flags().StringVarP(&o.PluginDir, "plugin-dir", "p", "plugins/managed", "The path where binary plugins are located")
	cmd.Flags().StringVarP(&o.Version, "version", "", "", "Install specific plugin version (if not passed, installs latest plugin version or the only available one)")
}

func (o *Options) run(args []string) error {
	log := o.globalFlags.GetLogger()

	manifestMap, err := plugin.BuildManifestMap(log, args[0], o.Repo)
	if err != nil {
		return nil
	}

	path := BuildInstallBinaryPath(o.PluginDir)
	installVersion := "latest"
	if o.Version != "" {
		installVersion = o.Version
	}

	if len(manifestMap) > 1 {
		// if the plugin is found across multiple repository then fail and ask for a specific repo
		log.Errorf(fmt.Sprintf("The plugin %s is found across multiple repos, please specify one repo with --repo flag", args[0]))
	} else if len(manifestMap) == 1 {
		// the plugin is found in only one repo
		for repo, manifest := range manifestMap {
			// install the only available version of the plugin
			if len(manifest) == 1 {
				for _, value := range manifest {
					// check if the version is mentioned and matches the version in manifest file
					if value.Name != "" && (o.Version == "" || string(value.Version) == o.Version) {
						err := DownloadBinary(fmt.Sprintf("%s/%s", path, repo), value.Name, value.Binaries[0].URI)
						if err != nil {
							return err
						}
						fmt.Println(fmt.Sprintf("Adding plugin %s from repo %s", args[0], repo))
						log.Infof("plugin %s added to the path - %s", args[0], fmt.Sprintf("%s/%s", path, repo))
						return nil
					} else {
						log.Errorf(fmt.Sprintf("The version %s of plugin %s is not available", o.Version, args[0]))
					}
				}
			} else if len(manifest) > 1 {
				// if there are multiple version of the plugins are available then look for the latest or mentioned version and if not found fail and ask user to input a version using --version flag
				for repo, manifest := range manifestMap {
					for _, value := range manifest {
						if string(value.Version) == installVersion {
							err := DownloadBinary(fmt.Sprintf("%s/%s", path, repo), value.Name, value.Binaries[0].URI)
							if err != nil {
								return err
							}
							fmt.Println(fmt.Sprintf("Adding plugin %s from repo %s", args[0], repo))
							log.Infof("plugin %s added to the path - %s", args[0], fmt.Sprintf("%s/%s", path, repo))
							return nil
						}
					}
					log.Errorf(fmt.Sprintf("The %s version of the plugin %s is not found, please mention one version using --version", installVersion, args[0]))
				}
			} else {
				// throw error saying that the plugin doest exists
				log.Errorf(fmt.Sprintf("The plugin %s is not found", args[0]))
			}
		}
	} else {
		// throw error saying that the plugin doest exists
		log.Errorf(fmt.Sprintf("The plugin %s is not found", args[0]))
	}
	return nil
}

func DownloadBinary(filepath string, filename string, url string) error {

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Create dir if not exists
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		err = os.MkdirAll(filepath, os.ModePerm)
		if err != nil {
			return err
		}
	}

	// Create the file
	out, err := os.OpenFile(filepath+"/"+filename, syscall.O_RDWR|syscall.O_CREAT|syscall.O_TRUNC, 0777)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return err
}

func BuildInstallBinaryPath(path string) string {
	pluginDir := "plugins/managed"

	if path != "" {
		pluginDir = path
	}

	return pluginDir
}
