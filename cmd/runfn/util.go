package runfn

import (
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"strings"
)

// WriteOutput Write the resource to the output directory.
// If the output directory is not specified, the resource is written to stdout.
func WriteOutput(outDir string, content string) error {
	r := strings.NewReader(content)
	var outputs []kio.Writer
	outDir, err := GetDestinationDir(outDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	outputs = []kio.Writer{kio.LocalPackageWriter{PackagePath: outDir}}
	return kio.Pipeline{
		Inputs:  []kio.Reader{&kio.ByteReader{Reader: r}},
		Outputs: outputs}.Execute()
}

// GetDestinationDir returns the destination directory, if outDir is not defined it creates a temp dir.
func GetDestinationDir(outDir string) (string, error) {
	if outDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %v", err)
		}
		dir, err := ioutil.TempDir(cwd, "crane_output")
		if err != nil {
			return "", err
		}
		outDir = dir
	}
	return outDir, nil
}

// ValidateFunctionImageURL validates the function name.
// According to Docker implementation
// https://github.com/docker/distribution/blob/master/reference/reference.go. A valid
// name definition is:
//	name                            := [domain '/'] path-component ['/' path-component]*
//	domain                          := domain-component ['.' domain-component]* [':' port-number]
//	domain-component                := /([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])/
//	port-number                     := /[0-9]+/
//	path-component                  := alpha-numeric [separator alpha-numeric]*
// 	alpha-numeric                   := /[a-z0-9]+/
//	separator                       := /[_.]|__|[-]*/
// https://github.com/GoogleContainerTools/kpt/blob/b197de30601072d7b8668dd41150f398a7f415f5/pkg/api/kptfile/v1/validation.go#L120-L150
func ValidateFunctionImageURL(name string) error {
	pathComponentRegexp := `(?:[a-z0-9](?:(?:[_.]|__|[-]*)[a-z0-9]+)*)`
	domainComponentRegexp := `(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])`
	domainRegexp := fmt.Sprintf(`%s(?:\.%s)*(?:\:[0-9]+)?`, domainComponentRegexp, domainComponentRegexp)
	nameRegexp := fmt.Sprintf(`(?:%s\/)?%s(?:\/%s)*`, domainRegexp,
		pathComponentRegexp, pathComponentRegexp)
	tagRegexp := `(?:[\w][\w.-]{0,127})`
	shaRegexp := `(sha256:[a-zA-Z0-9]{64})`
	versionRegexp := fmt.Sprintf(`(%s|%s)`, tagRegexp, shaRegexp)
	t := fmt.Sprintf(`^(?:%s(?:(\:|@)%s)?)$`, nameRegexp, versionRegexp)

	matched, err := regexp.MatchString(t, name)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("function name %q is invalid", name)
	}
	return nil
}

// Check if Directory exists
func checkIfDirExists(dir string) bool {
	_, err := os.Stat(dir)
	if err == nil || os.IsExist(err) {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}
