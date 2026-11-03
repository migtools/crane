package plugin

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/konveyor/crane-lib/transform"
	binary_plugin "github.com/konveyor/crane-lib/transform/binary-plugin"
	"github.com/konveyor/crane-lib/transform/kubernetes"
	"github.com/sirupsen/logrus"
)

const (
	DefaultLocalPluginDir = "/.local/share/crane/plugins"
	GlobalPluginDir       = "/usr/local/share/crane/plugins"
	PkgPluginDir          = "/usr/share/crane/plugins"
)

type PluginSource string

const (
	PluginSourceEmbedded PluginSource = "embedded"
	PluginSourceExternal PluginSource = "external"
)

// PluginDescriptor describes a discovered plugin and its transform behavior.
type PluginDescriptor struct {
	Plugin           transform.Plugin
	Source           PluginSource
	SourceDirectory  string
	EnabledByDefault bool
}

type builtInPlugin struct {
	plugin           transform.Plugin
	enabledByDefault bool
}

func getBuiltInPlugins(_ *logrus.Logger) []builtInPlugin {
	return []builtInPlugin{
		{plugin: &kubernetes.KubernetesTransformPlugin{}, enabledByDefault: true},
	}
}

func GetPlugins(dir string, logger *logrus.Logger) ([]transform.Plugin, error) {
	pluginList := []transform.Plugin{}
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
	descriptors, err := GetPluginDescriptors(pluginDir, skipPlugins, logger)
	if err != nil {
		return nil, err
	}

	plugins := make([]transform.Plugin, len(descriptors))
	for i, descriptor := range descriptors {
		plugins[i] = descriptor.Plugin
	}
	return plugins, nil
}

// GetPluginDescriptors returns the winning plugin from each discovery source.
// Earlier sources take precedence when multiple plugins have the same name.
func GetPluginDescriptors(pluginDir string, skipPlugins []string, logger *logrus.Logger) ([]PluginDescriptor, error) {
	absPathPluginDir, err := filepath.Abs("plugins")
	if err != nil {
		return nil, err
	}

	paths := []string{absPathPluginDir, pluginDir, GlobalPluginDir, PkgPluginDir}
	return discoverPluginDescriptors(getBuiltInPlugins(logger), paths, skipPlugins, func(path string) ([]transform.Plugin, error) {
		return GetPlugins(path, logger)
	})
}

func discoverPluginDescriptors(builtIns []builtInPlugin, paths, skipPlugins []string, loadPlugins func(string) ([]transform.Plugin, error)) ([]PluginDescriptor, error) {
	descriptors := make([]PluginDescriptor, 0, len(builtIns))
	seen := make(map[string]struct{}, len(builtIns))
	for _, builtIn := range builtIns {
		name := builtIn.plugin.Metadata().Name
		if _, exists := seen[name]; exists {
			continue
		}
		descriptors = append(descriptors, PluginDescriptor{
			Plugin:           builtIn.plugin,
			Source:           PluginSourceEmbedded,
			EnabledByDefault: builtIn.enabledByDefault,
		})
		seen[name] = struct{}{}
	}

	for _, path := range paths {
		plugins, err := loadPlugins(path)
		if err != nil {
			return nil, err
		}
		for _, discoveredPlugin := range plugins {
			name := discoveredPlugin.Metadata().Name
			if _, exists := seen[name]; exists {
				continue
			}
			descriptors = append(descriptors, PluginDescriptor{
				Plugin:           discoveredPlugin,
				Source:           PluginSourceExternal,
				SourceDirectory:  path,
				EnabledByDefault: true,
			})
			seen[name] = struct{}{}
		}
	}

	if len(skipPlugins) == 0 {
		return descriptors, nil
	}

	filteredDescriptors := make([]PluginDescriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if !isPluginInList(descriptor.Plugin, skipPlugins) {
			filteredDescriptors = append(filteredDescriptors, descriptor)
		}
	}
	return filteredDescriptors, nil
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
