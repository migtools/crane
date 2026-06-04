package version

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/konveyor/crane/internal/buildinfo"
)

func TestVersionOutput(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	o := &Options{}
	err := o.run()

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("run() returned error: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "crane:") {
		t.Error("output should contain 'crane:' header")
	}
	if !strings.Contains(output, "crane-lib:") {
		t.Error("output should contain 'crane-lib:' header")
	}
	if !strings.Contains(output, "kustomize:") {
		t.Error("output should contain 'kustomize:' header")
	}
	if !strings.Contains(output, buildinfo.Version) {
		t.Errorf("output should contain crane version %q", buildinfo.Version)
	}
	if !strings.Contains(output, buildinfo.KustomizeVersion) {
		t.Errorf("output should contain kustomize version %q", buildinfo.KustomizeVersion)
	}
}
