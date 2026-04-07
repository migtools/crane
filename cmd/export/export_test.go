package export

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes/fake"
)

func TestComplete_AsExtras(t *testing.T) {
	tests := []struct {
		name     string
		asExtras string
		wantKeys []string
		wantVals map[string][]string
		wantErr  bool
	}{
		{
			name:     "empty extras, no parsing",
			asExtras: "",
			wantKeys: nil,
		},
		{
			name:     "single key single value",
			asExtras: "key1=val1",
			wantVals: map[string][]string{"key1": {"val1"}},
		},
		{
			name:     "single key multiple values",
			asExtras: "key1=val1,val2",
			wantVals: map[string][]string{"key1": {"val1", "val2"}},
		},
		{
			name:     "multiple keys",
			asExtras: "key1=val1;key2=val2,val3",
			wantVals: map[string][]string{
				"key1": {"val1"},
				"key2": {"val2", "val3"},
			},
		},
		{
			name:     "bad format no equals",
			asExtras: "key1val1",
			wantErr:  true,
		},
		{
			name:     "bad format multiple equals",
			asExtras: "key1=val1=extra",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &ExportOptions{
				configFlags: genericclioptions.NewConfigFlags(true),
				asExtras:    tt.asExtras,
			}

			// Point configFlags to a non-existent kubeconfig so Complete
			// can still load the default (empty) config without a real cluster.
			empty := ""
			o.configFlags.KubeConfig = &empty

			err := o.Complete(nil, nil)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantVals != nil {
				if o.extras == nil {
					t.Fatal("extras map is nil")
				}
				for k, wantV := range tt.wantVals {
					gotV, ok := o.extras[k]
					if !ok {
						t.Fatalf("missing key %q in extras", k)
					}
					if len(gotV) != len(wantV) {
						t.Fatalf("extras[%q] = %v, want %v", k, gotV, wantV)
					}
					for i := range wantV {
						if gotV[i] != wantV[i] {
							t.Fatalf("extras[%q][%d] = %q, want %q", k, i, gotV[i], wantV[i])
						}
					}
				}
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name           string
		asExtras       string
		labelSelector  string
		impersonate    string
		impersonateGrp []string
		wantErr        bool
	}{
		{
			name:     "no extras, no impersonation - ok",
			asExtras: "",
		},
		{
			name:        "extras with impersonate user - ok",
			asExtras:    "key=val",
			impersonate: "admin",
		},
		{
			name:           "extras with impersonate group - ok",
			asExtras:       "key=val",
			impersonateGrp: []string{"devs"},
		},
		{
			name:     "extras without impersonation - error",
			asExtras: "key=val",
			wantErr:  true,
		},
		{
			name:          "empty label selector - ok",
			labelSelector: "",
		},
		{
			name:          "valid label selector equality - ok",
			labelSelector: "app=nginx",
		},
		{
			name:          "valid label selector set-based - ok",
			labelSelector: "env in (prod,staging)",
		},
		{
			name:          "invalid label selector - error",
			labelSelector: "key in (unclosed",
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &ExportOptions{
				configFlags:   genericclioptions.NewConfigFlags(true),
				asExtras:      tt.asExtras,
				labelSelector: tt.labelSelector,
			}
			o.configFlags.Impersonate = &tt.impersonate
			o.configFlags.ImpersonateGroup = &tt.impersonateGrp

			err := o.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestNewExportCommand(t *testing.T) {
	streams := genericclioptions.NewTestIOStreamsDiscard()
	cmd := NewExportCommand(streams, nil)

	if cmd.Use != "export" {
		t.Fatalf("Use = %q, want %q", cmd.Use, "export")
	}

	// Verify all expected flags are registered.
	expectedFlags := []string{
		"export-dir", "label-selector", "crd-skip-group",
		"crd-include-group", "as-extras", "qps", "burst",
	}
	for _, name := range expectedFlags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q not registered on export command", name)
		}
	}

	// Verify defaults.
	if d := cmd.Flags().Lookup("export-dir").DefValue; d != "export" {
		t.Errorf("export-dir default = %q, want %q", d, "export")
	}
	if d := cmd.Flags().Lookup("qps").DefValue; d != "100" {
		t.Errorf("qps default = %q, want %q", d, "100")
	}
	if d := cmd.Flags().Lookup("burst").DefValue; d != "1000" {
		t.Errorf("burst default = %q, want %q", d, "1000")
	}
}

func TestValidateExportNamespace(t *testing.T) {
	t.Parallel()

	t.Run("missing namespace returns not found message", func(t *testing.T) {
		t.Parallel()
		client := fake.NewClientset()
		err := validateExportNamespace(context.Background(), client, "non-existent-namespace")
		if err == nil {
			t.Fatal("expected error")
		}
		want := `namespaces "non-existent-namespace" not found`
		if err.Error() != want {
			t.Fatalf("error = %q, want %q", err.Error(), want)
		}
	})

	t.Run("existing namespace ok", func(t *testing.T) {
		t.Parallel()
		client := fake.NewClientset(&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "app-ns"},
		})
		if err := validateExportNamespace(context.Background(), client, "app-ns"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty namespace", func(t *testing.T) {
		t.Parallel()
		client := fake.NewClientset()
		err := validateExportNamespace(context.Background(), client, "")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
