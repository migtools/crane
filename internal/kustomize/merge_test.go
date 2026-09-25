package kustomize

import (
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const baseKustomization = `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- input/a.yaml
- input/b.yaml
patches:
- path: patches/p1.yaml
  target:
    kind: Deployment
    name: web
`

// unmarshalYAML is a small helper to turn merged bytes back into a generic map.
func unmarshalYAML(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("failed to unmarshal merged YAML: %v\n%s", err, data)
	}
	return out
}

func TestParseFragment(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
		check   func(t *testing.T, m map[string]interface{})
	}{
		{
			name: "json object",
			raw:  `{"namespace": "dest-ns", "commonLabels": {"app": "crane"}}`,
			check: func(t *testing.T, m map[string]interface{}) {
				if m["namespace"] != "dest-ns" {
					t.Errorf("namespace: got %v", m["namespace"])
				}
			},
		},
		{
			name: "yaml object",
			raw:  "namespace: dest-ns\ncommonLabels:\n  app: crane\n",
			check: func(t *testing.T, m map[string]interface{}) {
				if m["namespace"] != "dest-ns" {
					t.Errorf("namespace: got %v", m["namespace"])
				}
			},
		},
		{
			name:    "empty",
			raw:     "   ",
			wantErr: "empty",
		},
		{
			name:    "list root",
			raw:     `["a", "b"]`,
			wantErr: "got []interface {}",
		},
		{
			name:    "scalar root",
			raw:     `just-a-string`,
			wantErr: "must be a mapping",
		},
		{
			name:    "invalid yaml",
			raw:     "namespace: : :\n  - broken",
			wantErr: "invalid kustomize fragment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := ParseFragment(tt.raw)
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
				tt.check(t, m)
			}
		})
	}
}

func TestMergeFragment_AddsScalarAndMapFields(t *testing.T) {
	fragment := map[string]interface{}{
		"namespace":    "dest-ns",
		"commonLabels": map[string]interface{}{"app": "crane"},
		"namePrefix":   "pre-",
	}

	out, err := MergeFragment([]byte(baseKustomization), fragment)
	if err != nil {
		t.Fatalf("MergeFragment failed: %v", err)
	}
	m := unmarshalYAML(t, out)

	if m["namespace"] != "dest-ns" {
		t.Errorf("namespace: got %v", m["namespace"])
	}
	if m["namePrefix"] != "pre-" {
		t.Errorf("namePrefix: got %v", m["namePrefix"])
	}
	labels, ok := m["commonLabels"].(map[string]interface{})
	if !ok || labels["app"] != "crane" {
		t.Errorf("commonLabels: got %v", m["commonLabels"])
	}
	// Base fields preserved.
	if m["kind"] != "Kustomization" {
		t.Errorf("kind changed: got %v", m["kind"])
	}
}

func TestMergeFragment_AppendsResources(t *testing.T) {
	remote := "https://github.com/example/manifests//base?ref=v1.0.0"
	fragment := map[string]interface{}{
		"resources": []interface{}{remote, remote},
	}

	out, err := MergeFragment([]byte(baseKustomization), fragment)
	if err != nil {
		t.Fatalf("MergeFragment failed: %v", err)
	}
	m := unmarshalYAML(t, out)

	resources, ok := m["resources"].([]interface{})
	if !ok {
		t.Fatalf("resources not a list: %T", m["resources"])
	}
	// Duplicate fragment resources are appended once.
	want := []string{"input/a.yaml", "input/b.yaml", remote}
	if len(resources) != len(want) {
		t.Fatalf("expected %d resources, got %d: %v", len(want), len(resources), resources)
	}
	for i, w := range want {
		if resources[i] != w {
			t.Errorf("resources[%d]: expected %q, got %v", i, w, resources[i])
		}
	}
}

func TestMergeFragment_AppendsPatches(t *testing.T) {
	fragment := map[string]interface{}{
		"patches": []interface{}{
			map[string]interface{}{
				"patch": "- op: replace\n  path: /spec/type\n  value: ClusterIP\n",
				"target": map[string]interface{}{
					"kind": "Service",
					"name": "web",
				},
			},
		},
	}

	out, err := MergeFragment([]byte(baseKustomization), fragment)
	if err != nil {
		t.Fatalf("MergeFragment failed: %v", err)
	}
	m := unmarshalYAML(t, out)

	patches, ok := m["patches"].([]interface{})
	if !ok {
		t.Fatalf("patches not a list: %T", m["patches"])
	}
	if len(patches) != 2 {
		t.Fatalf("expected 2 patches, got %d: %v", len(patches), patches)
	}
}

func TestValidateFragmentPaths(t *testing.T) {
	stageDir := filepath.Join(t.TempDir(), "transform", "10_test")
	tests := []struct {
		name     string
		fragment map[string]interface{}
		wantErr  string
	}{
		{
			name:     "resource inside stage",
			fragment: map[string]interface{}{"resources": []interface{}{"extra/deployment.yaml"}},
			wantErr:  "inside generated stage directory",
		},
		{
			name:     "resource outside stage",
			fragment: map[string]interface{}{"resources": []interface{}{"../../shared/base"}},
		},
		{
			name:     "remote resource",
			fragment: map[string]interface{}{"resources": []interface{}{"oci://registry.example.com/manifests:v1"}},
		},
		{
			name: "patch inside stage",
			fragment: map[string]interface{}{"patches": []interface{}{
				map[string]interface{}{"path": "patches/update.yaml"},
			}},
			wantErr: "inside generated stage directory",
		},
		{
			name: "patch outside stage",
			fragment: map[string]interface{}{"patches": []interface{}{
				map[string]interface{}{"path": "../../shared/update.yaml"},
			}},
		},
		{
			name: "inline patch",
			fragment: map[string]interface{}{"patches": []interface{}{
				map[string]interface{}{"patch": "- op: replace\n  path: /spec/replicas\n  value: 2"},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFragmentPaths(stageDir, tt.fragment)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestMergeFragment_IgnoresProtectedFields(t *testing.T) {
	fragment := map[string]interface{}{
		"apiVersion": "evil/v1",
		"kind":       "NotKustomization",
		"namespace":  "dest-ns",
	}

	out, err := MergeFragment([]byte(baseKustomization), fragment)
	if err != nil {
		t.Fatalf("MergeFragment failed: %v", err)
	}
	m := unmarshalYAML(t, out)

	if m["apiVersion"] != "kustomize.config.k8s.io/v1beta1" {
		t.Errorf("apiVersion overridden: got %v", m["apiVersion"])
	}
	if m["kind"] != "Kustomization" {
		t.Errorf("kind overridden: got %v", m["kind"])
	}
	if m["namespace"] != "dest-ns" {
		t.Errorf("namespace not applied: got %v", m["namespace"])
	}
}

func TestMergeFragment_EmptyFragmentReturnsBase(t *testing.T) {
	out, err := MergeFragment([]byte(baseKustomization), nil)
	if err != nil {
		t.Fatalf("MergeFragment failed: %v", err)
	}
	if string(out) != baseKustomization {
		t.Errorf("expected base unchanged, got:\n%s", out)
	}
}

func TestMergeFragment_RejectsNonListFields(t *testing.T) {
	tests := []struct {
		name     string
		fragment map[string]interface{}
		wantType string
	}{
		{
			name:     "scalar resources",
			fragment: map[string]interface{}{"resources": "input/c.yaml"},
			wantType: "string",
		},
		{
			name:     "null resources",
			fragment: map[string]interface{}{"resources": nil},
			wantType: "<nil>",
		},
		{
			name:     "null patches",
			fragment: map[string]interface{}{"patches": nil},
			wantType: "<nil>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MergeFragment([]byte(baseKustomization), tt.fragment)
			if err == nil {
				t.Fatal("expected error for non-list field, got nil")
			}
			if !strings.Contains(err.Error(), "must be a list") {
				t.Fatalf("expected 'must be a list' error, got %v", err)
			}
			if !strings.Contains(err.Error(), "got "+tt.wantType) {
				t.Fatalf("expected error to include received type %q, got %v", tt.wantType, err)
			}
		})
	}
}
