package framework

import "testing"

func TestStripKubectlWarnings(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain output unchanged",
			input: "pod/redis-0",
			want:  "pod/redis-0",
		},
		{
			name:  "strips Warning prefix",
			input: "Warning: resource 'namespaces' is not namespace scoped\npod/redis-0",
			want:  "pod/redis-0",
		},
		{
			name:  "strips klog error line",
			input: "E1012 08:43:54.084796   65479 websocket.go:296] Unknown stream id 1, discarding message",
			want:  "",
		},
		{
			name:  "strips klog warning line",
			input: "W1012 08:43:54.084796   65479 websocket.go:296] some warning",
			want:  "",
		},
		{
			name:  "strips klog info line",
			input: "I1012 08:43:54.084796   65479 reflector.go:123] info message",
			want:  "",
		},
		{
			name:  "strips klog fatal line",
			input: "F1012 08:43:54.084796   65479 main.go:10] fatal message",
			want:  "",
		},
		{
			name:  "mixed klog and real output",
			input: "E1012 08:43:54.084796   65479 websocket.go:296] Unknown stream id 1\ndata-file.txt\nW0101 12:00:00.000000   1 foo.go:1] bar",
			want:  "data-file.txt",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "does not strip regular E prefix",
			input: "Error: something went wrong",
			want:  "Error: something went wrong",
		},
		{
			name:  "does not strip E followed by digits without timestamp",
			input: "E2024-report.txt",
			want:  "E2024-report.txt",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := StripKubectlWarnings(tc.input)
			if got != tc.want {
				t.Errorf("StripKubectlWarnings(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
