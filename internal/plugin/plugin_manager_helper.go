package plugin

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/sirupsen/logrus"
)

const (
	DEFAULT_REPO     = "default"
	DEFAULT_REPO_URL = "DEFAULT_REPO_URL"
	DEFAULT_URL      = "https://raw.githubusercontent.com/konveyor/crane-plugins/main/index.yaml"
)

// returns map containing the manifests with the key as name-version. Takes name and repo as input to filter accordingly
func BuildManifestMap(log *logrus.Logger, name string, repoName string) (map[string]map[string][]PluginVersion, error) {
	// TODO: for multiple repo, read values from conf file to this map
	repos := make(map[string]string)

	if repoName != "" {
		// read the repo and url from the conf file and update the map with the same
		// repos[repoName] = <repoUrl>
		log.Errorf("Multiple repository is not supported right now so the flag --repo will not work till next release")
		return nil, errors.New("multiple repository is not supported right now so the flag --repo will not work till next release")
	} else {
		// read the whole config file and iterate through all repos to make sure every manifest is read
		repos[DEFAULT_REPO] = GetDefaultSource()
	}
	manifestMap := make(map[string]map[string][]PluginVersion)

	// iterate over all the repos
	for repo, url := range repos {
		// get the index.yml file for respective repo
		index, err := GetYamlFromUrl(url)
		if err != nil {
			return nil, err
		}
		// fetch all the manifest file from a repo
		for _, p := range index.Plugins {
			// retrieve the manifest if name matches or there is no name passed, i.e a specific or all of the manifest
			if name == "" || strings.Contains(p.Name, name) {
				plugin, err := YamlToManifest(p.Path)
				if err != nil {
					log.Errorf("Error reading %s plugin manifest located at %s - Error: %s", p.Name, p.Path, err)
					return nil, err
				}
				if _, ok := manifestMap[repo]; ok {
					manifestMap[repo][p.Name] = plugin
				} else {
					manifestMap[repo] = make(map[string][]PluginVersion)
					manifestMap[repo][p.Name] = plugin
				}
			}
		}
	}
	return manifestMap, nil
}

// takes url as input and returns index.yml for plugin repository
func GetYamlFromUrl(URL string) (PluginIndex, error) {
	var manifest PluginIndex
	index, err := getData(URL)
	if err != nil {
		return manifest, err
	}
	err = yaml.Unmarshal(index, &manifest)
	if err != nil {
		return manifest, err
	}
	return manifest, nil
}

// takes url as input and fetches the manifest of a plugin
func YamlToManifest(URL string) ([]PluginVersion, error) {
	plugin := Plugin{}

	body, err := getData(URL)
	if err != nil {
		return plugin.Versions, err
	}

	err = yaml.Unmarshal(body, &plugin)
	if err != nil {
		return []PluginVersion{}, err
	}

	isPluginAvailable := FilterPluginForOsArch(&plugin)
	if isPluginAvailable {
		return plugin.Versions, nil
	}
	// TODO: figure out a better way to not return the plugin
	return []PluginVersion{}, nil
}

// takes manifest as input and filters manifest for current os/arch
func FilterPluginForOsArch(plugin *Plugin) bool {
	// filter manifests for current os/arch
	isPluginAvailable := false
	for _, version := range plugin.Versions {
		for _, binary := range version.Binaries {
			if binary.OS == runtime.GOOS && binary.Arch == runtime.GOARCH {
				isPluginAvailable = true
				version.Binaries = []Binary{
					binary,
				}
				break
			}
		}
	}
	return isPluginAvailable
}

// overrides the default plugin dir url
func GetDefaultSource() string {
	val, present := os.LookupEnv(DEFAULT_REPO_URL)
	if present {
		return val
	}
	return DEFAULT_URL
}

// return array of string containing all the paths where a binary installed within plugin-dir
func LocateBinaryInPluginDir(pluginDir string, name string, files []os.FileInfo) ([]string, error) {
	paths := []string{}

	for _, file := range files {
		filePath := fmt.Sprintf("%v/%v", pluginDir, file.Name())
		if file.Mode().IsRegular() && IsExecAny(file.Mode().Perm()) && file.Name() == name {
			paths = append(paths, filePath)
		}
	}
	return paths, nil
}

func IsUrl(URL string) (bool, string) {
	URL = strings.TrimPrefix(URL, "file://")
	u, err := url.Parse(URL)
	return err == nil && u.Scheme != "" && u.Host != "", URL
}

func getData(URL string) ([]byte, error) {
	var index []byte
	var err error
	isUrl, URL := IsUrl(URL)
	if !isUrl {
		index, err = ioutil.ReadFile(URL)
		if err != nil {
			return nil, err
		}
	} else {
		res, err := http.Get(URL)
		if err != nil {
			return nil, err
		}

		defer res.Body.Close()

		index, err = ioutil.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
	}
	return index, nil
}
