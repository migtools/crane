package plugin

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/konveyor/crane-lib/transform"
	binary_plugin "github.com/konveyor/crane-lib/transform/binary-plugin"
	"github.com/konveyor/crane-lib/transform/kubernetes"
	"github.com/sirupsen/logrus"
)

func GetPlugins(dir string, logger *logrus.Logger) ([]transform.Plugin, error) {
	pluginList := []transform.Plugin{&kubernetes.KubernetesTransformPlugin{}}
	files, err := ioutil.ReadDir(dir)
	switch {
	case os.IsNotExist(err):
		return pluginList, nil
	case err != nil:
		return nil, err
	}
	list, err := getBinaryPlugins(dir, files, logger)
	if err != nil {
		return nil, err
	}
	pluginList = append(pluginList, list...)
	return pluginList, nil
}

func getBinaryPlugins(path string, files []os.FileInfo, logger *logrus.Logger) ([]transform.Plugin, error) {
	pluginList := []transform.Plugin{}
	for _, file := range files {
		filePath := fmt.Sprintf("%v/%v", path, file.Name())
		if file.IsDir() {
			newFiles, err := ioutil.ReadDir(filePath)
			if err != nil {
				return nil, err
			}
			plugins, err := getBinaryPlugins(filePath, newFiles, logger)
			if err != nil {
				return nil, err
			}
			pluginList = append(pluginList, plugins...)
		} else if file.Mode().IsRegular() && IsExecAny(file.Mode().Perm()) {
			newPlugin, err := binary_plugin.NewBinaryPlugin(filePath, logger)
			if err != nil {
				return nil, err
			}
			pluginList = append(pluginList, newPlugin)
		}
	}
	return pluginList, nil
}

func IsExecAny(mode os.FileMode) bool {
	return mode&0111 != 0
}

func GetFilteredPlugins(pluginDir string, skipPlugins []string, logger *logrus.Logger) ([]transform.Plugin, error) {
	var filteredPlugins []transform.Plugin
	plugins, err := GetPlugins(pluginDir, logger)
	if err != nil {
		return filteredPlugins, err
	}
	if len(skipPlugins) == 0 {
		return plugins, nil
	}
	for _, thisPlugin := range plugins {
		if !isPluginInList(thisPlugin, skipPlugins) {
			filteredPlugins = append(filteredPlugins, thisPlugin)
		}
	}
	return filteredPlugins, nil
}

func isPluginInList(plugin transform.Plugin, list []string) bool {
	pluginName := plugin.Metadata().Name
	for _, listItem := range list {
		if pluginName == listItem {
			return true
		}
	}
	return false
}
