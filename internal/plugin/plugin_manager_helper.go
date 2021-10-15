package plugin

import (
	"errors"
	"fmt"
	"github.com/ghodss/yaml"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
	"os"
	"runtime"
	"strings"
)

const (
	DEFAULT_REPO     = "default"
	DEFAULT_REPO_URL = "DEFAULT_REPO_URL"
	DEFAULT_URL      = "https://raw.githubusercontent.com/konveyor/crane-plugins/main/index.yml"
)

type Manifest struct {
	Name             string   `json:"name"`
	ShortDescription string   `json:"shortDescription"`
	Description      string   `json:"description"`
	Version          Version  `json:"version"`
	Binaries         []Binary `json:"binaries"`
}

type Binary struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	URI  string `json:"uri"`
	SHA  string `json:"sha,omitempty"`
}

type Version string

func BuildManifestMap(log *logrus.Logger, name string, repoName string) (map[string]map[string]Manifest, error) {
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
	manifestMap := make(map[string]map[string]Manifest)

	for repo, url := range repos {
		urlMap, err := GetYamlFromUrl(url)
		if err != nil {
			return nil, err
		}
		manifestMap[repo] = make(map[string]Manifest)
		for key, value := range urlMap {
			if s, ok := value.(string); ok {
				if name == "" || strings.Contains(key, name) {
					plugin, err := YamlToManifest(s)
					if err != nil {
						log.Errorf("Error reading %s plugin manifest located at %s - Error: %s", key, s, err)
						return nil, err
					} else {
						manifestMap[repo][key] = plugin
					}
				}
			}
		}
	}
	return manifestMap, nil
}

func GetYamlFromUrl(url string) (map[string]interface{}, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)

	if err != nil {
		return nil, err
	}
	var manifest map[string]interface{}
	err = yaml.Unmarshal(body, &manifest)
	if err != nil {
		panic(err)
	}
	return manifest, nil
}

func YamlToManifest(url string) (Manifest, error) {
	plugin := Manifest{}
	res, err := http.Get(url)
	if err != nil {
		return plugin, err
	}

	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)

	if err != nil {
		return plugin, err
	}
	err = yaml.Unmarshal(body, &plugin)
	isPluginAvailable := FilterPluginForOsArch(&plugin)
	if isPluginAvailable {
		return plugin, nil
	}
	// TODO: figure out a better way to not return the plugin
	return Manifest{}, nil
}

func FilterPluginForOsArch(plugin *Manifest) bool {
	// filter manifests for current os/arch
	isPluginAvailable := false
	for _, binary := range plugin.Binaries {
		if binary.OS == runtime.GOOS && binary.Arch == runtime.GOARCH {
			isPluginAvailable = true
			plugin.Binaries = []Binary{
				binary,
			}
			break
		}
	}
	return isPluginAvailable
}

func GetDefaultSource() string {
	val, present := os.LookupEnv(DEFAULT_REPO_URL)
	if present {
		return val
	}
	return DEFAULT_URL
}

func LocateBinaryInPluginDir(pluginDir string, name string, files []os.FileInfo) ([]string, error) {
	paths := []string{}

	for _, file := range files {
		filePath := fmt.Sprintf("%v/%v", pluginDir, file.Name())
		if file.IsDir() {
			newFiles, err := ioutil.ReadDir(filePath)
			if err != nil {
				return nil, err
			}
			plugins, err := LocateBinaryInPluginDir(filePath, name, newFiles)
			if err != nil {
				return nil, err
			}
			paths = append(paths, plugins...)
		} else if file.Mode().IsRegular() && IsExecAny(file.Mode().Perm()) && file.Name() == name {
			paths = append(paths, filePath)
		}
	}
	return paths, nil
}
