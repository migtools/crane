package file

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
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
		return nil, fmt.Errorf("failed to read directory %q: %w", dir, err)
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
				return nil, fmt.Errorf("failed to read directory %q: %w", filePath, err)
			}
			files, err := readFiles(ctx, filePath, newFiles, log)
			if err != nil {
				return nil, err
			}
			jsonFiles = append(jsonFiles, files...)
		} else {
			data, err := ioutil.ReadFile(filePath)
			if err != nil {
				return nil, fmt.Errorf("failed to read file %q: %w", filePath, err)
			}
			json, err := yaml.YAMLToJSON(data)
			if err != nil {
				return nil, fmt.Errorf("failed to parse YAML in file %q: %w", filePath, err)
			}

			u := unstructured.Unstructured{}
			err = u.UnmarshalJSON(json)
			if err != nil {
				return nil, fmt.Errorf("file %q is not a valid Kubernetes resource: %w", filePath, err)
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

// Kustomize Layout Path Helpers

// GetStageDir returns the path to a stage directory
// Format: <transformDir>/<stageName>
func (opts *PathOpts) GetStageDir(stageName string) string {
	return filepath.Join(opts.TransformDir, stageName)
}

// GetResourcesDir returns the path to the resources directory within a stage
// Format: <transformDir>/<stageName>/resources
func (opts *PathOpts) GetResourcesDir(stageName string) string {
	return filepath.Join(opts.GetStageDir(stageName), "resources")
}

// GetPatchesDir returns the path to the patches directory within a stage
// Format: <transformDir>/<stageName>/patches
func (opts *PathOpts) GetPatchesDir(stageName string) string {
	return filepath.Join(opts.GetStageDir(stageName), "patches")
}

// GetReportsDir returns the path to the reports directory within a stage
// Format: <transformDir>/<stageName>/reports
func (opts *PathOpts) GetReportsDir(stageName string) string {
	return filepath.Join(opts.GetStageDir(stageName), "reports")
}

// GetWhiteoutsDir returns the path to the whiteouts directory within a stage
// Format: <transformDir>/<stageName>/whiteouts
func (opts *PathOpts) GetWhiteoutsDir(stageName string) string {
	return filepath.Join(opts.GetStageDir(stageName), "whiteouts")
}

// GetKustomizationPath returns the path to kustomization.yaml within a stage
// Format: <transformDir>/<stageName>/kustomization.yaml
func (opts *PathOpts) GetKustomizationPath(stageName string) string {
	return filepath.Join(opts.GetStageDir(stageName), "kustomization.yaml")
}

// GetMetadataPath returns the path to .crane-metadata.json within a stage
// Format: <transformDir>/<stageName>/.crane-metadata.json
func (opts *PathOpts) GetMetadataPath(stageName string) string {
	return filepath.Join(opts.GetStageDir(stageName), ".crane-metadata.json")
}

// GetResourceTypeFilePath returns the path to a resource type file within a stage
// Format: <transformDir>/<stageName>/resources/<filename>
func (opts *PathOpts) GetResourceTypeFilePath(stageName, filename string) string {
	return filepath.Join(opts.GetResourcesDir(stageName), filename)
}

// GetPatchFilePath returns the path to a patch file within a stage
// Format: <transformDir>/<stageName>/patches/<filename>
func (opts *PathOpts) GetPatchFilePath(stageName, filename string) string {
	return filepath.Join(opts.GetPatchesDir(stageName), filename)
}

// GetWhiteoutReportPath returns the path to the whiteout report file
// Format: <transformDir>/<stageName>/whiteouts/whiteouts.json
func (opts *PathOpts) GetWhiteoutReportPath(stageName string) string {
	return filepath.Join(opts.GetWhiteoutsDir(stageName), "whiteouts.json")
}

// GetIgnoredPatchReportPath returns the path to the ignored patches report file
// Format: <transformDir>/<stageName>/reports/ignored-patches.json
func (opts *PathOpts) GetIgnoredPatchReportPath(stageName string) string {
	return filepath.Join(opts.GetReportsDir(stageName), "ignored-patches.json")
}

// GetStageWorkDir returns the path to the working directory for a stage
// Format: <transformDir>/.work/<stageName>
func (opts *PathOpts) GetStageWorkDir(stageName string) string {
	return filepath.Join(opts.TransformDir, ".work", stageName)
}

// GetStageInputDir returns the path to the input directory for a stage
// Format: <transformDir>/.work/<stageName>/input
func (opts *PathOpts) GetStageInputDir(stageName string) string {
	return filepath.Join(opts.GetStageWorkDir(stageName), "input")
}

// GetStageTransformDir returns the path to the transform directory for a stage
// This is the actual stage directory containing kustomization.yaml
// Format: <transformDir>/<stageName>
func (opts *PathOpts) GetStageTransformDir(stageName string) string {
	return opts.GetStageDir(stageName)
}

// GetStageOutputDir returns the path to the output directory for a stage
// Format: <transformDir>/.work/<stageName>/output
func (opts *PathOpts) GetStageOutputDir(stageName string) string {
	return filepath.Join(opts.GetStageWorkDir(stageName), "output")
}

// GetKustomizeCommand returns the appropriate command (kubectl or oc) for kustomize
func GetKustomizeCommand() string {
	// Try kubectl first
	cmd := exec.Command("kubectl", "version", "--client")
	if err := cmd.Run(); err == nil {
		return "kubectl"
	}

	// Fallback to oc
	cmd = exec.Command("oc", "version", "--client")
	if err := cmd.Run(); err == nil {
		return "oc"
	}

	// Default to kubectl (will fail later with appropriate error)
	return "kubectl"
}

// GetResourceFilename returns a stable filename from kind, group, version, namespace, and name.
// Format matches export: Kind_group_version_namespace_name.yaml
// Examples: "ConfigMap__v1_default_my-config.yaml", "Deployment_apps_v1_default_my-app.yaml"
func GetResourceFilename(obj unstructured.Unstructured) string {
	namespace := obj.GetNamespace()
	if namespace == "" {
		namespace = "clusterscoped"
	}
	return strings.Join([]string{
		obj.GetKind(),
		obj.GetObjectKind().GroupVersionKind().GroupKind().Group,
		obj.GetObjectKind().GroupVersionKind().Version,
		namespace,
		obj.GetName(),
	}, "_") + ".yaml"
}
