package transform

import (
	"strings"
	"testing"
)

func TestParseStageKustomize(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		wantErr string
		check   func(t *testing.T, result map[string]map[string]interface{})
	}{
		{
			name:   "single stage json",
			values: []string{`KubernetesPlugin={"namespace": "dest-ns", "commonLabels": {"app": "crane"}}`},
			check: func(t *testing.T, result map[string]map[string]interface{}) {
				frag := result["KubernetesPlugin"]
				if frag["namespace"] != "dest-ns" {
					t.Errorf("namespace: got %v", frag["namespace"])
				}
			},
		},
		{
			name:   "single stage yaml",
			values: []string{"CustomEdits=namespace: dest-ns\ncommonLabels:\n  app: crane\n"},
			check: func(t *testing.T, result map[string]map[string]interface{}) {
				frag := result["CustomEdits"]
				if frag["namespace"] != "dest-ns" {
					t.Errorf("namespace: got %v", frag["namespace"])
				}
			},
		},
		{
			name: "multiple stages",
			values: []string{
				`KubernetesPlugin={"namespace": "a"}`,
				`RegistryPlugin={"namespace": "b"}`,
			},
			check: func(t *testing.T, result map[string]map[string]interface{}) {
				if len(result) != 2 {
					t.Errorf("expected 2 stages, got %d", len(result))
				}
			},
		},
		{
			name:    "missing equals sign",
			values:  []string{"KubernetesPlugin"},
			wantErr: "expected format StageName=YAML",
		},
		{
			name:    "empty stage name",
			values:  []string{`={"namespace": "x"}`},
			wantErr: "stage name is empty",
		},
		{
			name:    "empty fragment",
			values:  []string{`KubernetesPlugin=`},
			wantErr: "empty",
		},
		{
			name:    "list fragment rejected",
			values:  []string{`KubernetesPlugin=["a", "b"]`},
			wantErr: "must be a mapping",
		},
		{
			name: "duplicate stage",
			values: []string{
				`KubernetesPlugin={"namespace": "a"}`,
				`KubernetesPlugin={"namespace": "b"}`,
			},
			wantErr: "duplicate",
		},
		{
			name:   "fragment with commas is not split",
			values: []string{`KubernetesPlugin={"images": [{"name": "a", "newName": "b"}], "namespace": "ns"}`},
			check: func(t *testing.T, result map[string]map[string]interface{}) {
				frag := result["KubernetesPlugin"]
				if frag["namespace"] != "ns" {
					t.Errorf("namespace: got %v", frag["namespace"])
				}
				if _, ok := frag["images"].([]interface{}); !ok {
					t.Errorf("images: expected list, got %T", frag["images"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseStageKustomize(tt.values)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}
