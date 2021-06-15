package file

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

type File struct {
	Info         os.FileInfo
	Unstructured unstructured.Unstructured
}

func ReadFiles(dir string) ([]File, error) {
	log := logrus.New()

	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	return readFiles(dir, files, log)
}

func readFiles(path string, files []os.FileInfo, log *logrus.Logger) ([]File, error) {
	jsonFiles := []File{}
	for _, file := range files {
		filePath := fmt.Sprintf("%v/%v", path, file.Name())
		if file.IsDir() {
			newFiles, err := ioutil.ReadDir(filePath)
			if err != nil {
				return nil, err
			}
			files, err := readFiles(filePath, newFiles, log)
			if err != nil {
				return nil, err
			}
			jsonFiles = append(jsonFiles, files...)
		} else {
			data, err := ioutil.ReadFile(filePath)
			if err != nil {
				return nil, err
			}
			json, err := yaml.YAMLToJSON(data)
			if err != nil {
				return nil, err
			}

			u := unstructured.Unstructured{}
			u.UnmarshalJSON(json)

			jsonFiles = append(jsonFiles, File{
				Info:         file,
				Unstructured: u,
			})
		}
	}
	return jsonFiles, nil
}

//TODO: @shawn-hurley Add errors for these methods to validate that the correct struct values are set.
type PathOpts struct {
	TransformDir string
	ResourceDir  string
	OutputDir    string
}

func (opts *PathOpts) GetWhiteOutFilePath(filePath string) string {
	return opts.updatePath(".wh.", filePath)
}

func (opts *PathOpts) GetTransformPath(filePath string) string {
	return opts.updatePath("transform-", filePath)
}

func (opts *PathOpts) updatePath(prefix, filePath string) string {
	dir, fname := filepath.Split(filePath)
	dir = strings.Replace(dir, opts.ResourceDir, opts.TransformDir, 1)
	fname = fmt.Sprintf("%v%v", prefix, fname)
	return filepath.Join(dir, fname)
}

func (opts *PathOpts) GetOutputFilePath(filePath string) string {
	dir, fname := filepath.Split(filePath)
	dir = strings.Replace(dir, opts.ResourceDir, opts.OutputDir, 1)
	return filepath.Join(dir, fname)
}
