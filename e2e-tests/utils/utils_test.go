package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseYAMLDocuments(t *testing.T) {
	// Table-driven checks for valid/invalid YAML streams.
	cases := []struct {
		name     string
		data     []byte
		wantLen  int
		wantErr  bool
		wantKind string
	}{
		{
			name:     "single_document",
			data:     []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: x\n"),
			wantLen:  1,
			wantKind: "Pod",
		},
		{
			name:    "multi_document",
			data:    []byte("a: 1\n---\nb: 2\n"),
			wantLen: 2,
		},
		{
			name:    "empty_input",
			data:    nil,
			wantLen: 0,
		},
		{
			name:    "invalid_yaml",
			data:    []byte("{\n"),
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			docs, err := parseYAMLDocuments(tc.data)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseYAMLDocuments: %v", err)
			}
			if len(docs) != tc.wantLen {
				t.Fatalf("len(docs) = %d, want %d", len(docs), tc.wantLen)
			}
			if tc.wantKind != "" {
				m, ok := docs[0].(map[string]any)
				if !ok {
					t.Fatalf("root type = %T, want map[string]any", docs[0])
				}
				if m["kind"] != tc.wantKind {
					t.Fatalf("kind = %v, want %s", m["kind"], tc.wantKind)
				}
			}
		})
	}
}

func TestCompareYAMLFileBytes(t *testing.T) {
	// Table-driven checks for semantic equality and parser error context.
	cases := []struct {
		name        string
		relPath     string
		golden      []byte
		got         []byte
		wantErr     bool
		errContains []string
	}{
		{
			name:    "equivalent_yaml_semantics",
			relPath: "cm.yaml",
			golden:  []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\ndata:\n  k: v\n"),
			got:     []byte("{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\",\"metadata\":{\"name\":\"x\"},\"data\":{\"k\":\"v\"}}"),
		},
		{
			name:        "semantic_mismatch_returns_diff",
			relPath:     "cm.yaml",
			golden:      []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n"),
			got:         []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: b\n"),
			wantErr:     true,
			errContains: []string{"YAML differs in", "cm.yaml"},
		},
		{
			name:        "invalid_golden_yaml",
			relPath:     "bad.yaml",
			golden:      []byte("{\n"),
			got:         []byte("a: 1\n"),
			wantErr:     true,
			errContains: []string{"parse golden file"},
		},
		{
			name:        "invalid_got_yaml",
			relPath:     "bad.yaml",
			golden:      []byte("a: 1\n"),
			got:         []byte("{\n"),
			wantErr:     true,
			errContains: []string{"parse got file"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := compareYAMLFileBytes(tc.relPath, tc.golden, tc.got)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				for _, s := range tc.errContains {
					if !strings.Contains(err.Error(), s) {
						t.Fatalf("error %q does not contain %q", err.Error(), s)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("compareYAMLFileBytes: %v", err)
			}
		})
	}
}

func TestListFilesRecursivelyAsList(t *testing.T) {
	// Verify recursive listing returns sorted, relative paths.
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		path    string
		content string
	}{
		{filepath.Join(root, "root-a.txt"), "a"},
		{filepath.Join(sub, "nested-b.txt"), "b"},
		{filepath.Join(sub, "nested-c.txt"), "c"},
	} {
		if err := os.WriteFile(p.path, []byte(p.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ListFilesRecursivelyAsList(root)
	if err != nil {
		t.Fatalf("ListFilesRecursivelyAsList: %v", err)
	}

	want := []string{
		"root-a.txt",
		filepath.Join("sub", "nested-b.txt"),
		filepath.Join("sub", "nested-c.txt"),
	}
	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Fatalf("ListFilesRecursivelyAsList(%q) = %q, want %q", root, got, want)
	}

	// Run with: go test ./e2e-tests/utils -run TestListFilesRecursivelyAsList -v
	t.Logf("return type: []string (sorted relative paths from the directory you pass in)")
	t.Logf("len=%d", len(got))
	t.Logf("Go value: %#v", got)
	jsonBytes, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("JSON:\n%s", jsonBytes)
	t.Logf("one entry per line:\n%s", strings.Join(got, "\n"))
}

func TestCompareDirectoryFileSets(t *testing.T) {
	// Table-driven checks for directory validation and file-set mismatch behavior.
	buildSameDirCase := func(t *testing.T) (string, string) {
		t.Helper()
		goldenDir, err := GoldenManifestsDir("simple-nginx-nopv", "export")
		if err != nil {
			t.Fatalf("GoldenManifestsDir(%q, %q): %v", "simple-nginx-nopv", "export", err)
		}
		return goldenDir, goldenDir
	}
	buildMismatchCase := func(t *testing.T) (string, string) {
		t.Helper()
		a := t.TempDir()
		b := t.TempDir()
		if err := os.WriteFile(filepath.Join(a, "only-a.txt"), []byte("a"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(b, "only-b.txt"), []byte("b"), 0o644); err != nil {
			t.Fatal(err)
		}
		return a, b
	}
	buildGoldenFileCase := func(t *testing.T) (string, string) {
		t.Helper()
		f := filepath.Join(t.TempDir(), "file.txt")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return f, t.TempDir()
	}
	buildGotFileCase := func(t *testing.T) (string, string) {
		t.Helper()
		f := filepath.Join(t.TempDir(), "file.txt")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return t.TempDir(), f
	}

	cases := []struct {
		name        string
		build       func(t *testing.T) (string, string)
		wantErr     bool
		errContains []string
	}{
		{name: "same_dir", build: buildSameDirCase},
		{
			name:        "mismatch",
			build:       buildMismatchCase,
			wantErr:     true,
			errContains: []string{"file sets differ", "only-a.txt", "only-b.txt"},
		},
		{
			name:        "golden_not_directory",
			build:       buildGoldenFileCase,
			wantErr:     true,
			errContains: []string{"not a directory"},
		},
		{
			name:        "got_not_directory",
			build:       buildGotFileCase,
			wantErr:     true,
			errContains: []string{"not a directory"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			goldenDir, gotDir := tc.build(t)
			err := CompareDirectoryFileSets(goldenDir, gotDir)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				for _, s := range tc.errContains {
					if !strings.Contains(err.Error(), s) {
						t.Fatalf("error %q does not contain %q", err.Error(), s)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("CompareDirectoryFileSets: %v", err)
			}
		})
	}
}

func TestGoldenManifestsDir(t *testing.T) {
	// Table-driven checks for valid/invalid app+stage inputs.
	cases := []struct {
		name    string
		appName string
		stage   string
		wantErr bool
	}{
		{name: "app1_export", appName: "app1", stage: "export"},
		{name: "simple-nginx-nopv_output", appName: "simple-nginx-nopv", stage: "output"},
		{name: "empty_app", appName: "", stage: "output", wantErr: true},
		{name: "invalid_stage", appName: "app", stage: "invalid", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := GoldenManifestsDir(tc.appName, tc.stage)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("GoldenManifestsDir(%q, %q): want error", tc.appName, tc.stage)
				}
				return
			}
			if err != nil {
				t.Fatalf("GoldenManifestsDir(%q, %q): %v", tc.appName, tc.stage, err)
			}
			wantSuffix := filepath.Join("golden-manifests", tc.appName, tc.stage)
			clean := filepath.Clean(got)
			if !strings.HasSuffix(clean, wantSuffix) {
				t.Fatalf("GoldenManifestsDir(%q, %q) = %q, want path ending with %q", tc.appName, tc.stage, got, wantSuffix)
			}
		})
	}
}

func TestCreateTempDir(t *testing.T) {
	// Ensure temp dir creation succeeds and preserves the requested prefix.
	dir, err := CreateTempDir("utils-test-")
	if err != nil {
		t.Fatalf("CreateTempDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("os.Stat(%q): %v", dir, err)
	}
	if !strings.Contains(filepath.Base(dir), "utils-test-") {
		t.Fatalf("temp dir %q does not include prefix %q", dir, "utils-test-")
	}
}

func TestListFilesRecursively(t *testing.T) {
	// Validate human-readable listing for empty and nested directory layouts.
	cases := []struct {
		name        string
		build       func(t *testing.T) string
		errContains []string
	}{
		{
			name: "empty_dir",
			build: func(t *testing.T) string {
				return t.TempDir()
			},
			errContains: []string{"(no files)"},
		},
		{
			name: "nested_files_listed",
			build: func(t *testing.T) string {
				root := t.TempDir()
				nested := filepath.Join(root, "nested")
				if err := os.MkdirAll(nested, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(nested, "b.txt"), []byte("b"), 0o644); err != nil {
					t.Fatal(err)
				}
				return root
			},
			errContains: []string{"a.txt", filepath.Join("nested", "b.txt")},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.build(t)
			out, err := ListFilesRecursively(dir)
			if err != nil {
				t.Fatalf("ListFilesRecursively(%q): %v", dir, err)
			}
			for _, s := range tc.errContains {
				if !strings.Contains(out, s) {
					t.Fatalf("output %q does not contain %q", out, s)
				}
			}
		})
	}
}

func TestHasFilesRecursively(t *testing.T) {
	// Confirm boolean/file-listing behavior for empty vs populated directories.
	cases := []struct {
		name      string
		build     func(t *testing.T) string
		wantFiles bool
	}{
		{
			name: "no_files",
			build: func(t *testing.T) string {
				return t.TempDir()
			},
			wantFiles: false,
		},
		{
			name: "has_files",
			build: func(t *testing.T) string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			wantFiles: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.build(t)
			got, listing, err := HasFilesRecursively(dir)
			if err != nil {
				t.Fatalf("HasFilesRecursively(%q): %v", dir, err)
			}
			if got != tc.wantFiles {
				t.Fatalf("HasFilesRecursively(%q) = %v, want %v", dir, got, tc.wantFiles)
			}
			if listing == "" {
				t.Fatal("expected non-empty listing text")
			}
		})
	}
}

func TestReadTestdataFile(t *testing.T) {
	// Check both successful testdata reads and contextual read errors.
	cases := []struct {
		name        string
		filename    string
		wantErr     bool
		errContains []string
	}{
		{name: "existing_file", filename: "subscription.yaml"},
		{
			name:        "missing_file",
			filename:    "does-not-exist.yaml",
			wantErr:     true,
			errContains: []string{"failed to read file"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReadTestdataFile(tc.filename)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				for _, s := range tc.errContains {
					if !strings.Contains(err.Error(), s) {
						t.Fatalf("error %q does not contain %q", err.Error(), s)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadTestdataFile(%q): %v", tc.filename, err)
			}
			if strings.TrimSpace(got) == "" {
				t.Fatalf("ReadTestdataFile(%q): got empty content", tc.filename)
			}
		})
	}
}

func TestCompareDirectoryYAMLSemantics(t *testing.T) {
	// Exercise end-to-end directory semantics checks: match, semantic diff, and file-set mismatch.
	write := func(t *testing.T, dir, rel, content string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name        string
		build       func(t *testing.T) (string, string)
		wantErr     bool
		errContains []string
	}{
		{
			name: "semantic_match_ignores_formatting",
			build: func(t *testing.T) (string, string) {
				golden := t.TempDir()
				got := t.TempDir()
				write(t, golden, "resources/cm.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n")
				write(t, got, "resources/cm.yaml", "{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\",\"metadata\":{\"name\":\"x\"}}")
				return golden, got
			},
		},
		{
			name: "semantic_diff_detected",
			build: func(t *testing.T) (string, string) {
				golden := t.TempDir()
				got := t.TempDir()
				write(t, golden, "resources/cm.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n")
				write(t, got, "resources/cm.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: b\n")
				return golden, got
			},
			wantErr:     true,
			errContains: []string{"compare YAML file", "YAML differs"},
		},
		{
			name: "file_set_mismatch_detected",
			build: func(t *testing.T) (string, string) {
				golden := t.TempDir()
				got := t.TempDir()
				write(t, golden, "only-golden.yaml", "a: 1\n")
				write(t, got, "only-got.yaml", "a: 1\n")
				return golden, got
			},
			wantErr:     true,
			errContains: []string{"file sets differ"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			goldenDir, gotDir := tc.build(t)
			err := CompareDirectoryYAMLSemantics(goldenDir, gotDir)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				for _, s := range tc.errContains {
					if !strings.Contains(err.Error(), s) {
						t.Fatalf("error %q does not contain %q", err.Error(), s)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("CompareDirectoryYAMLSemantics: %v", err)
			}
		})
	}
}

func TestLooksLikeYAMLFile(t *testing.T) {
	// Accept yaml extensions (or no extension) and reject non-yaml extensions.
	cases := []struct {
		path string
		want bool
	}{
		{path: "a.yaml", want: true},
		{path: "a.yml", want: true},
		{path: "no-extension", want: true},
		{path: "notes.txt", want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			got := LooksLikeYAMLFile(tc.path)
			if got != tc.want {
				t.Fatalf("LooksLikeYAMLFile(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
