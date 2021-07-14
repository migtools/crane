package plugin

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane-lib/transform/binary-plugin"
)
func GetBinaryPlugins(dir string) ([]transform.Plugin, error) {
	files, err := ioutil.ReadDir(dir)
	switch {
	case os.IsNotExist(err):
		return []transform.Plugin{}, nil
	case err != nil:
		return nil, err
	}
	return getBinaryPlugins(dir, files)
}

func getBinaryPlugins(path string, files []os.FileInfo) ([]transform.Plugin, error) {
	pluginList := []transform.Plugin{}
	for _, file := range files {
		filePath := fmt.Sprintf("%v/%v", path, file.Name())
		if file.IsDir() {
			newFiles, err := ioutil.ReadDir(filePath)
			if err != nil {
				return nil, err
			}
			plugins, err := getBinaryPlugins(filePath, newFiles)
			if err != nil {
				return nil, err
			}
			pluginList = append(pluginList, plugins...)
		} else if file.Mode().IsRegular() && isExecAny(file.Mode().Perm()) {
			newPlugin, err := binary_plugin.NewBinaryPlugin(filePath)
			if err != nil {
				return nil, err
			}
			pluginList = append(pluginList, newPlugin)
		}
	}
	return pluginList, nil
}

func isExecAny(mode os.FileMode) bool {
    return mode&0111 != 0
}
