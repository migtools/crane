package file

import (
	"context"
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
	Path         string
}

func ReadFiles(ctx context.Context, dir string) ([]File, error) {
	log := logrus.New()

	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	return readFiles(ctx, dir, files, log)
}

func readFiles(ctx context.Context, path string, files []os.FileInfo, log *logrus.Logger) ([]File, error) {
	jsonFiles := []File{}
	for _, file := range files {
		filePath := fmt.Sprintf("%v/%v", path, file.Name())
		if file.IsDir() {
			if file.Name() == "failures" {
				continue
			}
			newFiles, err := ioutil.ReadDir(filePath)
			if err != nil {
				return nil, err
			}
			files, err := readFiles(ctx, filePath, newFiles, log)
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
			err = u.UnmarshalJSON(json)
			if err != nil {
				return nil, err
			}

			jsonFiles = append(jsonFiles, File{
				Info:         file,
				Unstructured: u,
				Path:         filePath,
			})
		}
	}
	return jsonFiles, nil
}

//TODO: @shawn-hurley Add errors for these methods to validate that the correct struct values are set.
type PathOpts struct {
	TransformDir      string
	ExportDir         string
	OutputDir         string
	IgnoredPatchesDir string
}

func (opts *PathOpts) GetWhiteOutFilePath(filePath string) string {
	return opts.updateTransformDirPath(".wh.", filePath)
}

func (opts *PathOpts) GetTransformPath(filePath string) string {
	return opts.updateTransformDirPath("transform-", filePath)
}

func (opts *PathOpts) GetIgnoredPatchesPath(filePath string) string {
	return opts.updateIgnoredPatchesDirPath("ignored-", filePath)
}

func (opts *PathOpts) updateTransformDirPath(prefix, filePath string) string {
	return opts.updatePath(opts.TransformDir, prefix, filePath)
}

func (opts *PathOpts) updateIgnoredPatchesDirPath(prefix, filePath string) string {
	if len(opts.IgnoredPatchesDir) == 0 {
		return ""
	}
	return opts.updatePath(opts.IgnoredPatchesDir, prefix, filePath)
}

func (opts *PathOpts) updatePath(updateDir, prefix, filePath string) string {
	dir, fname := filepath.Split(filePath)
	dir = strings.Replace(dir, opts.ExportDir, updateDir, 1)
	fname = fmt.Sprintf("%v%v", prefix, fname)
	return filepath.Join(dir, fname)
}

func (opts *PathOpts) GetOutputFilePath(filePath string) string {
	dir, fname := filepath.Split(filePath)
	dir = strings.Replace(dir, opts.ExportDir, opts.OutputDir, 1)
	return filepath.Join(dir, fname)
}
