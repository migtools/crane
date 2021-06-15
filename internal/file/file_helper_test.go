package file_test

import (
	"testing"

	"github.com/konveyor/crane/internal/file"
)

func TestGetWhiteOutFilePath(t *testing.T) {
	cases := []struct {
		Name        string
		Filepath    string
		Dir         string
		ResourceDir string
		Expected    string
	}{
		{
			Name:        "test whiteout file creation",
			Filepath:    "/fully/qualified/resources/ns/path-test",
			Dir:         "/fully/qualified/transform",
			ResourceDir: "/fully/qualified/resources",
			Expected:    "/fully/qualified/transform/ns/.wh.path-test",
		},
	}

	for _, test := range cases {
		opts := file.PathOpts{
			TransformDir: test.Dir,
			ResourceDir:  test.ResourceDir,
		}
		if actual := opts.GetWhiteOutFilePath(test.Filepath); actual != test.Expected {
			t.Errorf("actual: %v did not match expected: %v", actual, test.Expected)
		}
	}
}

func TestGetTransformPath(t *testing.T) {
	cases := []struct {
		Name        string
		Filepath    string
		Dir         string
		ResourceDir string
		Expected    string
	}{
		{
			Name:        "test transform file creation",
			Filepath:    "/fully/qualified/ns/path-test",
			Dir:         "/fully/qualified/transform",
			ResourceDir: "/fully/qualified",
			Expected:    "/fully/qualified/transform/ns/transform-path-test",
		},
	}
	for _, test := range cases {
		opts := file.PathOpts{
			TransformDir: test.Dir,
			ResourceDir:  test.ResourceDir,
		}
		if actual := opts.GetTransformPath(test.Filepath); actual != test.Expected {
			t.Errorf("actual: %v did not match expected: %v", actual, test.Expected)
		}
	}

}

func TestGetOutputFilePath(t *testing.T) {
	cases := []struct {
		Name        string
		Filepath    string
		Dir         string
		ResourceDir string
		Expected    string
	}{
		{
			Name:        "test transform file creation",
			Filepath:    "/fully/qualified/ns/path-test",
			Dir:         "/fully/qualified/output",
			ResourceDir: "/fully/qualified",
			Expected:    "/fully/qualified/output/ns/path-test",
		},
	}
	for _, test := range cases {
		opts := file.PathOpts{
			OutputDir:   test.Dir,
			ResourceDir: test.ResourceDir,
		}
		if actual := opts.GetOutputFilePath(test.Filepath); actual != test.Expected {
			t.Errorf("actual: %v did not match expected: %v", actual, test.Expected)
		}
	}
}
