package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
)

func testLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(os.Stderr)
	l.SetLevel(logrus.DebugLevel)
	return l
}

func TestScanManifests_SingleDoc(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "deploy.yaml"), `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: prod
`)
	entries, err := ScanManifests(ScanOptions{Dirs: []string{dir}}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Group != "apps" || e.Version != "v1" || e.Kind != "Deployment" || e.Namespace != "prod" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	if e.APIVersion != "apps/v1" {
		t.Fatalf("APIVersion = %q, want %q", e.APIVersion, "apps/v1")
	}
}

func TestScanManifests_MultiDoc(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "multi.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
  namespace: prod
---
apiVersion: v1
kind: Secret
metadata:
  name: s1
  namespace: prod
`)
	entries, err := ScanManifests(ScanOptions{Dirs: []string{dir}}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
}

func TestScanManifests_NestedDirs(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "a.yaml"), `
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: default
`)
	writeFile(t, filepath.Join(sub, "b.yaml"), `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
`)
	entries, err := ScanManifests(ScanOptions{Dirs: []string{dir}}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
}

func TestScanManifests_SkipsFailuresDir(t *testing.T) {
	dir := t.TempDir()
	failDir := filepath.Join(dir, "failures")
	if err := os.MkdirAll(failDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(failDir, "bad.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: should-skip
`)
	writeFile(t, filepath.Join(dir, "good.yaml"), `
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: default
`)
	entries, err := ScanManifests(ScanOptions{Dirs: []string{dir}}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (failures should be skipped)", len(entries))
	}
}

func TestScanManifests_NonYAMLIgnored(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "readme.txt"), "not yaml")
	writeFile(t, filepath.Join(dir, "data.csv"), "a,b,c")
	writeFile(t, filepath.Join(dir, "valid.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
`)
	entries, err := ScanManifests(ScanOptions{Dirs: []string{dir}}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
}

func TestScanManifests_Deduplication(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg1
  namespace: prod
`)
	writeFile(t, filepath.Join(dir, "b.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg2
  namespace: prod
`)
	entries, err := ScanManifests(ScanOptions{Dirs: []string{dir}}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (deduped by group/version/kind/namespace)", len(entries))
	}
	if len(entries[0].SourceFiles) != 2 {
		t.Fatalf("got %d source files, want 2", len(entries[0].SourceFiles))
	}
}

func TestScanManifests_CoreGroupParsing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pod.yaml"), `
apiVersion: v1
kind: Pod
metadata:
  name: p
  namespace: default
`)
	entries, err := ScanManifests(ScanOptions{Dirs: []string{dir}}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Group != "" {
		t.Fatalf("Group = %q, want empty string for core API", entries[0].Group)
	}
	if entries[0].Version != "v1" {
		t.Fatalf("Version = %q, want %q", entries[0].Version, "v1")
	}
}

func TestScanManifests_NamedGroupParsing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "deploy.yaml"), `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
`)
	entries, err := ScanManifests(ScanOptions{Dirs: []string{dir}}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Group != "apps" {
		t.Fatalf("Group = %q, want %q", entries[0].Group, "apps")
	}
	if entries[0].Version != "v1" {
		t.Fatalf("Version = %q, want %q", entries[0].Version, "v1")
	}
}

func TestScanManifests_ClusterScoped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "crb.yaml"), `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: my-binding
`)
	entries, err := ScanManifests(ScanOptions{Dirs: []string{dir}}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Namespace != "" {
		t.Fatalf("Namespace = %q, want empty for cluster-scoped", entries[0].Namespace)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
