package framework

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeKubectl writes a shell script standing in for kubectl and returns a
// KubectlRunner that invokes it.
func fakeKubectl(t *testing.T, script string) KubectlRunner {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake kubectl script requires a POSIX shell")
	}
	bin := filepath.Join(t.TempDir(), "kubectl")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatalf("write fake kubectl: %v", err)
	}
	return KubectlRunner{Bin: bin}
}

func TestVerifierPodDiagnostics(t *testing.T) {
	tests := []struct {
		name         string
		script       string
		wantContains []string
	}{
		{
			name: "status and logs available",
			script: `case "$1" in
get) echo 'phase=Failed containerStatuses=[{"state":{"terminated":{"exitCode":1,"reason":"Error"}}}]' ;;
logs) echo "Can't handle RDB format version 17" ;;
esac
`,
			wantContains: []string{
				"diagnostics for verifier pod ns/verifier-pod:",
				`status: phase=Failed containerStatuses=[{"state":{"terminated":{"exitCode":1,"reason":"Error"}}}]`,
				"logs (last 50 lines):\nCan't handle RDB format version 17",
			},
		},
		{
			name: "collection errors are reported inline",
			script: `echo 'Error from server (NotFound): pods "verifier-pod" not found'
exit 1
`,
			wantContains: []string{
				"diagnostics for verifier pod ns/verifier-pod:",
				"status: unavailable:",
				"logs: unavailable:",
				`pods "verifier-pod" not found`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifierPodDiagnostics(fakeKubectl(t, tt.script), "ns", "verifier-pod")
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("VerifierPodDiagnostics() missing %q in:\n%s", want, got)
				}
			}
		})
	}
}
