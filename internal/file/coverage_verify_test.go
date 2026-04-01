// DELETE THIS FILE — temporary test to verify CI coverage pipeline works.
package file

import "testing"

func TestCoverageVerify_PathOpts(t *testing.T) {
	opts := &PathOpts{
		TransformDir:      "/tmp/transform",
		ExportDir:         "/tmp/export",
		OutputDir:         "/tmp/output",
		IgnoredPatchesDir: "/tmp/ignored",
	}

	tests := []struct {
		name string
		fn   func(string) string
		in   string
		want string
	}{
		{"WhiteOutFilePath", opts.GetWhiteOutFilePath, "/tmp/export/ns/pod.yaml", "/tmp/transform/ns/.wh.pod.yaml"},
		{"TransformPath", opts.GetTransformPath, "/tmp/export/ns/pod.yaml", "/tmp/transform/ns/transform-pod.yaml"},
		{"IgnoredPatchesPath", opts.GetIgnoredPatchesPath, "/tmp/export/ns/pod.yaml", "/tmp/ignored/ns/ignored-pod.yaml"},
		{"OutputFilePath", opts.GetOutputFilePath, "/tmp/export/ns/pod.yaml", "/tmp/output/ns/pod.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn(tt.in)
			if got != tt.want {
				t.Errorf("%s(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
			}
		})
	}
}

func TestCoverageVerify_IgnoredPatchesEmpty(t *testing.T) {
	opts := &PathOpts{
		TransformDir: "/tmp/transform",
		ExportDir:    "/tmp/export",
		OutputDir:    "/tmp/output",
		// IgnoredPatchesDir left empty
	}

	got := opts.GetIgnoredPatchesPath("/tmp/export/ns/pod.yaml")
	if got != "" {
		t.Errorf("expected empty string when IgnoredPatchesDir is empty, got %q", got)
	}
}
