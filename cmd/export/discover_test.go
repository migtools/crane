package export

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	openapi_v2 "github.com/google/gnostic-models/openapiv2"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/openapi"
	restclient "k8s.io/client-go/rest"
	kubetesting "k8s.io/client-go/testing"
)

// ---------- getFilePath ----------

func TestGetFilePath(t *testing.T) {
	tests := []struct {
		name      string
		obj       unstructured.Unstructured
		wantParts []string // substrings that must appear
	}{
		{
			name: "namespaced object",
			obj: func() unstructured.Unstructured {
				u := unstructured.Unstructured{}
				u.SetKind("Deployment")
				u.SetName("web")
				u.SetNamespace("prod")
				u.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
				return u
			}(),
			wantParts: []string{"Deployment", "apps", "v1", "prod", "web", ".yaml"},
		},
		{
			name: "cluster-scoped object uses clusterscoped",
			obj: func() unstructured.Unstructured {
				u := unstructured.Unstructured{}
				u.SetKind("ClusterRole")
				u.SetName("admin")
				u.SetGroupVersionKind(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"})
				return u
			}(),
			wantParts: []string{"ClusterRole", "rbac.authorization.k8s.io", "v1", "clusterscoped", "admin", ".yaml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getFilePath(tt.obj)
			for _, p := range tt.wantParts {
				if !strings.Contains(got, p) {
					t.Errorf("getFilePath() = %q, missing %q", got, p)
				}
			}
		})
	}
}

// ---------- isAdmittedResource ----------

func TestIsAdmittedResource(t *testing.T) {
	tests := []struct {
		name     string
		gv       schema.GroupVersion
		resource metav1.APIResource
		want     bool
	}{
		{
			name:     "namespaced resource always admitted",
			gv:       schema.GroupVersion{Group: "apps", Version: "v1"},
			resource: metav1.APIResource{Kind: "Deployment", Namespaced: true},
			want:     true,
		},
		{
			name:     "cluster-scoped ClusterRoleBinding admitted",
			gv:       schema.GroupVersion{Group: "rbac.authorization.k8s.io", Version: "v1"},
			resource: metav1.APIResource{Kind: "ClusterRoleBinding", Namespaced: false},
			want:     true,
		},
		{
			name:     "cluster-scoped ClusterRole admitted",
			gv:       schema.GroupVersion{Group: "rbac.authorization.k8s.io", Version: "v1"},
			resource: metav1.APIResource{Kind: "ClusterRole", Namespaced: false},
			want:     true,
		},
		{
			name:     "cluster-scoped SCC admitted",
			gv:       schema.GroupVersion{Group: "security.openshift.io", Version: "v1"},
			resource: metav1.APIResource{Kind: "SecurityContextConstraints", Namespaced: false},
			want:     true,
		},
		{
			name:     "cluster-scoped Namespace not admitted",
			gv:       schema.GroupVersion{Group: "", Version: "v1"},
			resource: metav1.APIResource{Kind: "Namespace", Namespaced: false},
			want:     false,
		},
		{
			name:     "cluster-scoped Node not admitted",
			gv:       schema.GroupVersion{Group: "", Version: "v1"},
			resource: metav1.APIResource{Kind: "Node", Namespaced: false},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAdmittedResource(tt.gv, tt.resource)
			if got != tt.want {
				t.Fatalf("isAdmittedResource() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------- hasClusterScopedManifests ----------

func TestHasClusterScopedManifests(t *testing.T) {
	tests := []struct {
		name      string
		resources []*groupResource
		want      bool
	}{
		{
			name:      "nil slice",
			resources: nil,
			want:      false,
		},
		{
			name:      "empty slice",
			resources: []*groupResource{},
			want:      false,
		},
		{
			name: "nil groupResource entry",
			resources: []*groupResource{nil},
			want: false,
		},
		{
			name: "nil objects field",
			resources: []*groupResource{
				{APIResource: metav1.APIResource{Kind: "Deployment"}},
			},
			want: false,
		},
		{
			name: "only namespaced objects",
			resources: []*groupResource{
				{
					APIResource: metav1.APIResource{Kind: "Deployment"},
					objects: &unstructured.UnstructuredList{
						Items: []unstructured.Unstructured{
							namespacedObj("default", "web"),
						},
					},
				},
			},
			want: false,
		},
		{
			name: "has cluster-scoped object",
			resources: []*groupResource{
				{
					APIResource: metav1.APIResource{Kind: "ClusterRole"},
					objects: &unstructured.UnstructuredList{
						Items: []unstructured.Unstructured{
							clusterScopedObj("admin"),
						},
					},
				},
			},
			want: true,
		},
		{
			name: "mixed namespaced and cluster-scoped",
			resources: []*groupResource{
				{
					APIResource: metav1.APIResource{Kind: "Deployment"},
					objects: &unstructured.UnstructuredList{
						Items: []unstructured.Unstructured{namespacedObj("ns", "d1")},
					},
				},
				{
					APIResource: metav1.APIResource{Kind: "ClusterRole"},
					objects: &unstructured.UnstructuredList{
						Items: []unstructured.Unstructured{clusterScopedObj("cr1")},
					},
				},
			},
			want: true,
		},
		{
			name: "only CustomResourceDefinition cluster object",
			resources: []*groupResource{
				{
					APIResource: metav1.APIResource{Kind: "CustomResourceDefinition"},
					objects: &unstructured.UnstructuredList{
						Items: []unstructured.Unstructured{crdObj("widgets.example.com")},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasClusterScopedManifests(tt.resources)
			if got != tt.want {
				t.Fatalf("hasClusterScopedManifests() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------- prepareFailuresDir ----------

func TestPrepareFailuresDir(t *testing.T) {
	dir := t.TempDir()
	failuresDir := filepath.Join(dir, "failures")

	// First call creates it.
	if err := prepareFailuresDir(failuresDir); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := os.Stat(failuresDir); err != nil {
		t.Fatalf("directory not created: %v", err)
	}

	// Write a sentinel file inside.
	sentinel := filepath.Join(failuresDir, "stale.yaml")
	if err := os.WriteFile(sentinel, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}

	// Second call should remove the old content and recreate.
	if err := prepareFailuresDir(failuresDir); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("stale file should have been removed")
	}
	if _, err := os.Stat(failuresDir); err != nil {
		t.Fatalf("directory should exist after second call: %v", err)
	}
}

// ---------- prepareClusterResourceDir ----------

func TestPrepareClusterResourceDir(t *testing.T) {
	t.Run("no cluster manifests - dir not created", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "_cluster")
		resources := []*groupResource{
			{
				APIResource: metav1.APIResource{Kind: "Deployment"},
				objects: &unstructured.UnstructuredList{
					Items: []unstructured.Unstructured{namespacedObj("ns", "d1")},
				},
			},
		}
		if err := prepareClusterResourceDir(dir, resources); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatal("_cluster dir should not exist when there are no cluster-scoped manifests")
		}
	})

	t.Run("has cluster manifests - dir created", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "_cluster")
		resources := []*groupResource{
			{
				APIResource: metav1.APIResource{Kind: "ClusterRole"},
				objects: &unstructured.UnstructuredList{
					Items: []unstructured.Unstructured{clusterScopedObj("admin")},
				},
			},
		}
		if err := prepareClusterResourceDir(dir, resources); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("dir not created: %v", err)
		}
		if !info.IsDir() {
			t.Fatal("expected a directory")
		}
	})

	t.Run("stale content removed on re-run", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "_cluster")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		sentinel := filepath.Join(dir, "old.yaml")
		if err := os.WriteFile(sentinel, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}

		resources := []*groupResource{
			{
				APIResource: metav1.APIResource{Kind: "ClusterRole"},
				objects: &unstructured.UnstructuredList{
					Items: []unstructured.Unstructured{clusterScopedObj("admin")},
				},
			},
		}
		if err := prepareClusterResourceDir(dir, resources); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
			t.Fatal("stale file should have been removed")
		}
	})

	t.Run("CRD-only cluster manifests create dir", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "_cluster")
		resources := []*groupResource{
			{
				APIResource: metav1.APIResource{Kind: "Deployment"},
				objects: &unstructured.UnstructuredList{
					Items: []unstructured.Unstructured{namespacedObj("ns", "d1")},
				},
			},
			{
				APIResource: metav1.APIResource{
					Kind:         "CustomResourceDefinition",
					Name:         "customresourcedefinitions",
					SingularName: "customresourcedefinition",
					Namespaced:   false,
				},
				objects: &unstructured.UnstructuredList{
					Items: []unstructured.Unstructured{crdObj("widgets.example.com")},
				},
			},
		}
		if err := prepareClusterResourceDir(dir, resources); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("dir not created: %v", err)
		}
		if !info.IsDir() {
			t.Fatal("expected a directory")
		}
	})
}

// ---------- writeResources ----------

func TestWriteResources(t *testing.T) {
	log := testLogger()
	resourceDir := filepath.Join(t.TempDir(), "resources")
	clusterDir := filepath.Join(t.TempDir(), "_cluster")
	os.MkdirAll(resourceDir, 0700)
	os.MkdirAll(clusterDir, 0700)

	nsObj := namespacedObj("test-ns", "web")
	nsObj.SetKind("Deployment")
	nsObj.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})

	clObj := clusterScopedObj("admin-role")
	clObj.SetKind("ClusterRole")
	clObj.SetGroupVersionKind(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"})

	resources := []*groupResource{
		{
			APIResource: metav1.APIResource{Name: "deployments", Kind: "Deployment"},
			objects: &unstructured.UnstructuredList{
				Items: []unstructured.Unstructured{nsObj},
			},
		},
		{
			APIResource: metav1.APIResource{Name: "clusterroles", Kind: "ClusterRole"},
			objects: &unstructured.UnstructuredList{
				Items: []unstructured.Unstructured{clObj},
			},
		},
	}

	errs := writeResources(resources, clusterDir, resourceDir, log)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// Namespaced resource written to resourceDir.
	nsPath := filepath.Join(resourceDir, getFilePath(nsObj))
	if _, err := os.Stat(nsPath); err != nil {
		t.Errorf("namespaced resource file not found: %v", err)
	}

	// Cluster-scoped resource written to clusterDir.
	clPath := filepath.Join(clusterDir, getFilePath(clObj))
	if _, err := os.Stat(clPath); err != nil {
		t.Errorf("cluster-scoped resource file not found: %v", err)
	}
}

func TestWriteResources_SkipsEmptyKind(t *testing.T) {
	log := testLogger()
	dir := t.TempDir()

	resources := []*groupResource{
		{
			APIResource: metav1.APIResource{Name: "things", Kind: ""},
			objects: &unstructured.UnstructuredList{
				Items: []unstructured.Unstructured{namespacedObj("ns", "x")},
			},
		},
	}

	errs := writeResources(resources, dir, dir, log)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// No files should be written.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yaml") {
			t.Errorf("unexpected file written: %s", e.Name())
		}
	}
}

// ---------- writeErrors ----------

func TestWriteErrors(t *testing.T) {
	log := testLogger()
	dir := t.TempDir()

	errors := []*groupResourceError{
		{
			APIResource: metav1.APIResource{Name: "widgets", Kind: "Widget"},
			Error:       os.ErrPermission,
		},
	}

	errs := writeErrors(errors, dir, log)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	path := filepath.Join(dir, "widgets.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("error file not written: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("error file is empty")
	}
}

func TestWriteErrors_SkipsEmptyKind(t *testing.T) {
	log := testLogger()
	dir := t.TempDir()

	errors := []*groupResourceError{
		{
			APIResource: metav1.APIResource{Name: "things", Kind: ""},
			Error:       os.ErrPermission,
		},
	}

	errs := writeErrors(errors, dir, log)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yaml") {
			t.Errorf("unexpected error file written: %s", e.Name())
		}
	}
}

func TestPrepareFailuresDir_failsWhenPathUnderFile(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	failuresDir := filepath.Join(blocker, "failures")
	err := prepareFailuresDir(failuresDir)
	if err == nil {
		t.Fatal("expected error when failuresDir is under a file (RemoveAll fails first)")
	}
	if !strings.Contains(err.Error(), "failures export directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareFailuresDir_mkdirFailsWhenParentDirReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may create directories despite parent mode")
	}
	base := t.TempDir()
	if err := os.Chmod(base, 0444); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(base, 0700) }()
	failuresDir := filepath.Join(base, "failures")
	err := prepareFailuresDir(failuresDir)
	if err == nil {
		t.Fatal("expected error when parent directory is not writable (RemoveAll or MkdirAll)")
	}
	if !strings.Contains(err.Error(), "failures export directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Parent r-x (0555): RemoveAll on a missing child succeeds; MkdirAll fails with permission denied,
// exercising the create-failures branch (not the clear-* branch).
func TestPrepareFailuresDir_mkdirFailsWhenParentNotWritable_rwxTrick(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may bypass directory permissions")
	}
	base := t.TempDir()
	if err := os.Chmod(base, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(base, 0700) }()
	failuresDir := filepath.Join(base, "failures")
	err := prepareFailuresDir(failuresDir)
	if err == nil {
		t.Fatal("expected MkdirAll error")
	}
	if !strings.Contains(err.Error(), "create failures export directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareClusterResourceDir_failsWhenPathUnderFile(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	clusterDir := filepath.Join(blocker, "_cluster")
	resources := []*groupResource{
		{
			APIResource: metav1.APIResource{Kind: "ClusterRole"},
			objects: &unstructured.UnstructuredList{
				Items: []unstructured.Unstructured{clusterScopedObj("r")},
			},
		},
	}
	err := prepareClusterResourceDir(clusterDir, resources)
	if err == nil {
		t.Fatal("expected error when cluster dir is under a file")
	}
	if !strings.Contains(err.Error(), "cluster export directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareClusterResourceDir_mkdirFailsWhenParentDirReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may create directories despite parent mode")
	}
	base := t.TempDir()
	if err := os.Chmod(base, 0444); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(base, 0700) }()
	clusterDir := filepath.Join(base, "_cluster")
	resources := []*groupResource{
		{
			APIResource: metav1.APIResource{Kind: "ClusterRole"},
			objects: &unstructured.UnstructuredList{
				Items: []unstructured.Unstructured{clusterScopedObj("r")},
			},
		},
	}
	err := prepareClusterResourceDir(clusterDir, resources)
	if err == nil {
		t.Fatal("expected error when parent directory is not writable (RemoveAll or MkdirAll)")
	}
	if !strings.Contains(err.Error(), "cluster export directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareClusterResourceDir_mkdirFailsWhenParentNotWritable_rwxTrick(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may bypass directory permissions")
	}
	base := t.TempDir()
	if err := os.Chmod(base, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(base, 0700) }()
	clusterDir := filepath.Join(base, "_cluster")
	resources := []*groupResource{
		{
			APIResource: metav1.APIResource{Kind: "ClusterRole"},
			objects: &unstructured.UnstructuredList{
				Items: []unstructured.Unstructured{clusterScopedObj("r")},
			},
		},
	}
	err := prepareClusterResourceDir(clusterDir, resources)
	if err == nil {
		t.Fatal("expected MkdirAll error")
	}
	if !strings.Contains(err.Error(), "create cluster export directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareClusterResourceDir_removeAllFailsReadOnlyParent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can remove entries despite parent mode")
	}
	base := t.TempDir()
	clusterDir := filepath.Join(base, "_cluster")
	if err := os.MkdirAll(clusterDir, 0700); err != nil {
		t.Fatal(err)
	}
	resources := []*groupResource{
		{
			APIResource: metav1.APIResource{Kind: "ClusterRole"},
			objects: &unstructured.UnstructuredList{
				Items: []unstructured.Unstructured{clusterScopedObj("r")},
			},
		},
	}
	if err := os.Chmod(base, 0500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(base, 0700) }()

	err := prepareClusterResourceDir(clusterDir, resources)
	if err == nil {
		t.Fatal("expected error when RemoveAll cannot delete under read-only parent")
	}
	if !strings.Contains(err.Error(), "clear cluster export directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteResources_createFailsWhenTargetPathIsDirectory(t *testing.T) {
	log := testLogger()
	tmp := t.TempDir()
	resourceDir := filepath.Join(tmp, "out")
	clusterDir := filepath.Join(tmp, "_cluster")
	if err := os.MkdirAll(resourceDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(clusterDir, 0700); err != nil {
		t.Fatal(err)
	}

	nsObj := namespacedObj("ns", "pod-a")
	nsObj.SetKind("Pod")
	nsObj.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"})
	baseName := getFilePath(nsObj)
	if err := os.Mkdir(filepath.Join(resourceDir, baseName), 0700); err != nil {
		t.Fatal(err)
	}

	resources := []*groupResource{
		{
			APIResource: metav1.APIResource{Name: "pods", Kind: "Pod"},
			objects:     &unstructured.UnstructuredList{Items: []unstructured.Unstructured{nsObj}},
		},
	}
	errs := writeResources(resources, clusterDir, resourceDir, log)
	if len(errs) != 1 {
		t.Fatalf("want 1 error, got %v", errs)
	}
}

func TestWriteResources_marshalFails(t *testing.T) {
	log := testLogger()
	tmp := t.TempDir()
	resourceDir := filepath.Join(tmp, "out")
	clusterDir := filepath.Join(tmp, "_cluster")
	if err := os.MkdirAll(resourceDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(clusterDir, 0700); err != nil {
		t.Fatal(err)
	}
	nsObj := namespacedObj("ns", "x")
	nsObj.SetKind("Pod")
	nsObj.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "Pod"})
	nsObj.Object["bad"] = make(chan int)

	resources := []*groupResource{
		{
			APIResource: metav1.APIResource{Name: "pods", Kind: "Pod"},
			objects:     &unstructured.UnstructuredList{Items: []unstructured.Unstructured{nsObj}},
		},
	}
	errs := writeResources(resources, clusterDir, resourceDir, log)
	if len(errs) != 1 {
		t.Fatalf("want 1 marshal error, got %v", errs)
	}
}

func TestWriteResources_createFailsWhenDestinationFileNotWritable(t *testing.T) {
	log := testLogger()
	tmp := t.TempDir()
	resourceDir := filepath.Join(tmp, "out")
	clusterDir := filepath.Join(tmp, "_cluster")
	if err := os.MkdirAll(resourceDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(clusterDir, 0700); err != nil {
		t.Fatal(err)
	}

	nsObj := namespacedObj("ns", "pod-a")
	nsObj.SetKind("Pod")
	nsObj.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"})
	path := filepath.Join(resourceDir, getFilePath(nsObj))
	if err := os.WriteFile(path, []byte("seed"), 0400); err != nil {
		t.Fatal(err)
	}

	resources := []*groupResource{
		{
			APIResource: metav1.APIResource{Name: "pods", Kind: "Pod"},
			objects:     &unstructured.UnstructuredList{Items: []unstructured.Unstructured{nsObj}},
		},
	}
	errs := writeResources(resources, clusterDir, resourceDir, log)
	if len(errs) != 1 {
		t.Fatalf("want 1 error opening/truncating read-only destination file, got %v", errs)
	}
}

func TestWriteErrors_createFailsWhenFailuresDirParentIsFile(t *testing.T) {
	log := testLogger()
	base := t.TempDir()
	blocker := filepath.Join(base, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	failuresDir := filepath.Join(blocker, "failures")

	listErrs := []*groupResourceError{
		{APIResource: metav1.APIResource{Name: "widgets", Kind: "Widget"}, Error: errors.New("list failed")},
	}
	errs := writeErrors(listErrs, failuresDir, log)
	if len(errs) != 1 {
		t.Fatalf("want 1 error, got %v", errs)
	}
}

func TestWriteErrors_targetPathIsDirectory(t *testing.T) {
	log := testLogger()
	dir := t.TempDir()
	listErrs := []*groupResourceError{
		{APIResource: metav1.APIResource{Name: "widgets", Kind: "Widget"}, Error: errors.New("list failed")},
	}
	target := filepath.Join(dir, "widgets.yaml")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	errs := writeErrors(listErrs, dir, log)
	if len(errs) != 1 {
		t.Fatalf("want 1 error when error file path is a directory, got %v", errs)
	}
}

// jsonMarshalFailError makes json.Marshal (used by sigs.k8s.io/yaml.Marshal) fail for writeErrors.
type jsonMarshalFailError struct{}

func (jsonMarshalFailError) Error() string { return "boom" }

func (jsonMarshalFailError) MarshalJSON() ([]byte, error) {
	return nil, errors.New("marshal failed")
}

func TestWriteErrors_jsonMarshalFails(t *testing.T) {
	log := testLogger()
	dir := t.TempDir()
	listErrs := []*groupResourceError{
		{
			APIResource: metav1.APIResource{Name: "widgets", Kind: "Widget"},
			Error:       jsonMarshalFailError{},
		},
	}
	errs := writeErrors(listErrs, dir, log)
	if len(errs) != 1 {
		t.Fatalf("want 1 yaml/json marshal error, got %v", errs)
	}
}

// ---------- resourceToExtract ----------

func TestResourceToExtract_SkipsEvents(t *testing.T) {
	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClient(scheme)

	lists := []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "events", Kind: "Event", Namespaced: true, Verbs: metav1.Verbs{"list", "get"}},
			},
		},
	}

	resources, _ := resourceToExtract("default", "", client, lists, testLogger())
	for _, r := range resources {
		if r.APIResource.Kind == "Event" {
			t.Fatal("Event resources should be skipped")
		}
	}
}

func TestResourceToExtract_SkipsClusterScopedNonAdmitted(t *testing.T) {
	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClient(scheme)

	lists := []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "namespaces", Kind: "Namespace", Namespaced: false, Verbs: metav1.Verbs{"list", "get"}},
			},
		},
	}

	resources, _ := resourceToExtract("default", "", client, lists, testLogger())
	for _, r := range resources {
		if r.APIResource.Kind == "Namespace" {
			t.Fatal("Namespace resources should be skipped (not admitted)")
		}
	}
}

func TestResourceToExtract_SkipsEmptyVerbs(t *testing.T) {
	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClient(scheme)

	lists := []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: metav1.Verbs{}},
			},
		},
	}

	resources, _ := resourceToExtract("default", "", client, lists, testLogger())
	if len(resources) > 0 {
		t.Fatal("resources with empty verbs should be skipped")
	}
}

func TestResourceToExtract_SkipsEmptyAPIResources(t *testing.T) {
	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClient(scheme)

	lists := []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{},
		},
	}

	resources, errs := resourceToExtract("default", "", client, lists, testLogger())
	if len(resources) != 0 || len(errs) != 0 {
		t.Fatal("empty APIResources list should produce no resources or errors")
	}
}

// ---------- test discovery mock (implements discovery.DiscoveryInterface) ----------

type testDiscovery struct {
	serverPreferredResources []*metav1.APIResourceList
	serverPreferredErr       error
}

func (d *testDiscovery) RESTClient() restclient.Interface { return nil }

func (d *testDiscovery) ServerGroups() (*metav1.APIGroupList, error) {
	return nil, errors.New("not implemented")
}

func (d *testDiscovery) ServerResourcesForGroupVersion(string) (*metav1.APIResourceList, error) {
	return nil, errors.New("not implemented")
}

func (d *testDiscovery) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return nil, nil, errors.New("not implemented")
}

func (d *testDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	return d.serverPreferredResources, d.serverPreferredErr
}

func (d *testDiscovery) ServerPreferredNamespacedResources() ([]*metav1.APIResourceList, error) {
	return nil, errors.New("not implemented")
}

func (d *testDiscovery) ServerVersion() (*version.Info, error) {
	return nil, errors.New("not implemented")
}

func (d *testDiscovery) OpenAPISchema() (*openapi_v2.Document, error) {
	return nil, errors.New("not implemented")
}

func (d *testDiscovery) OpenAPIV3() openapi.Client { return nil }

func (d *testDiscovery) WithLegacy() discovery.DiscoveryInterface { return d }

// ---------- discoverPreferredResources ----------

func stdVerbs() metav1.Verbs {
	return metav1.Verbs{"list", "create", "get", "delete"}
}

func TestDiscoverPreferredResources_filtersVerbs(t *testing.T) {
	td := &testDiscovery{
		serverPreferredResources: []*metav1.APIResourceList{
			{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{
					{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: stdVerbs()},
					{Name: "secrets", Kind: "Secret", Namespaced: true, Verbs: metav1.Verbs{"list"}},
				},
			},
		},
	}
	out, err := discoverPreferredResources(td, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || len(out[0].APIResources) != 1 || out[0].APIResources[0].Name != "configmaps" {
		t.Fatalf("got %#v", out)
	}
}

func TestDiscoverPreferredResources_groupDiscoveryFailed_partialLists(t *testing.T) {
	partial := &discovery.ErrGroupDiscoveryFailed{
		Groups: map[schema.GroupVersion]error{
			{Group: "widgets.example.com", Version: "v1"}: errors.New("discovery failed"),
		},
	}
	td := &testDiscovery{
		serverPreferredErr: partial,
		serverPreferredResources: []*metav1.APIResourceList{
			{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{
					{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: stdVerbs()},
				},
			},
		},
	}
	out, err := discoverPreferredResources(td, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || len(out[0].APIResources) != 1 {
		t.Fatalf("expected filtered list, got %#v", out)
	}
}

func TestDiscoverPreferredResources_groupDiscoveryFailed_noLists(t *testing.T) {
	partial := &discovery.ErrGroupDiscoveryFailed{Groups: map[schema.GroupVersion]error{{Group: "x", Version: "v1"}: errors.New("fail")}}
	td := &testDiscovery{serverPreferredErr: partial, serverPreferredResources: nil}
	_, err := discoverPreferredResources(td, testLogger())
	if err == nil {
		t.Fatal("expected error")
	}
	if !discovery.IsGroupDiscoveryFailedError(err) {
		t.Fatalf("want ErrGroupDiscoveryFailed, got %v", err)
	}
}

func TestDiscoverPreferredResources_nonDiscoveryError(t *testing.T) {
	td := &testDiscovery{
		serverPreferredErr: fmt.Errorf("network error"),
		serverPreferredResources: []*metav1.APIResourceList{
			{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: stdVerbs()}}},
		},
	}
	_, err := discoverPreferredResources(td, testLogger())
	if err == nil || err.Error() != "network error" {
		t.Fatalf("got %v", err)
	}
}

func TestDiscoverPreferredResources_partialErrorAllResourcesFilteredOut(t *testing.T) {
	partial := &discovery.ErrGroupDiscoveryFailed{Groups: map[schema.GroupVersion]error{{Group: "x", Version: "v1"}: errors.New("fail")}}
	td := &testDiscovery{
		serverPreferredErr: partial,
		serverPreferredResources: []*metav1.APIResourceList{
			{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{
					{Name: "secrets", Kind: "Secret", Namespaced: true, Verbs: metav1.Verbs{"list"}},
				},
			},
		},
	}
	_, err := discoverPreferredResources(td, testLogger())
	if err == nil {
		t.Fatal("expected error when partial discovery and no resources pass verb filter")
	}
}

// ---------- getObjects ----------

func TestGetObjects_configMaps(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cm1"},
		Data:       map[string]string{"k": "v"},
	}
	client := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme, cm)
	g := &groupResource{
		APIGroup:        "",
		APIVersion:      "v1",
		APIGroupVersion: "v1",
		APIResource:     metav1.APIResource{Name: "configmaps", Kind: "ConfigMap", Namespaced: true},
	}
	list, err := getObjects(g, "default", "", client, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].GetName() != "cm1" {
		t.Fatalf("got %#v", list.Items)
	}
}

func TestGetObjects_imageStreamTagsUsesGetPerItem(t *testing.T) {
	ist := &unstructured.Unstructured{}
	ist.SetAPIVersion("image.openshift.io/v1")
	ist.SetKind("ImageStreamTag")
	ist.SetNamespace("openshift")
	ist.SetName("ruby:latest")
	if err := unstructured.SetNestedField(ist.Object, map[string]interface{}{"tag": map[string]interface{}{}}, "spec"); err != nil {
		t.Fatal(err)
	}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), ist)
	g := &groupResource{
		APIGroup:        "image.openshift.io",
		APIVersion:      "v1",
		APIGroupVersion: "image.openshift.io/v1",
		APIResource:     metav1.APIResource{Name: "imagestreamtags", Kind: "ImageStreamTag", Namespaced: true},
	}
	list, err := getObjects(g, "openshift", "", client, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].GetName() != "ruby:latest" {
		t.Fatalf("got %#v", list.Items)
	}
}

func TestGetObjects_imagetagResourceName(t *testing.T) {
	it := &unstructured.Unstructured{}
	it.SetAPIVersion("example.com/v1")
	it.SetKind("ImageTag")
	it.SetNamespace("ns1")
	it.SetName("img:1")
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), it)
	g := &groupResource{
		APIGroup:        "example.com",
		APIVersion:      "v1",
		APIGroupVersion: "example.com/v1",
		APIResource:     metav1.APIResource{Name: "imagetags", Kind: "ImageTag", Namespaced: true},
	}
	list, err := getObjects(g, "ns1", "", client, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("got %d items", len(list.Items))
	}
}

func TestGetObjects_passesLabelSelectorToList(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cm1", Labels: map[string]string{"app": "test"}},
		Data:       map[string]string{"k": "v"},
	}
	client := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme, cm)
	var sawLabel string
	client.PrependReactor("list", "configmaps", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		la, ok := action.(kubetesting.ListActionImpl)
		if !ok {
			t.Fatalf("expected ListActionImpl, got %T", action)
		}
		sawLabel = la.GetListOptions().LabelSelector
		return false, nil, nil
	})
	g := &groupResource{
		APIGroup:        "",
		APIVersion:      "v1",
		APIGroupVersion: "v1",
		APIResource:     metav1.APIResource{Name: "configmaps", Kind: "ConfigMap", Namespaced: true},
	}
	list, err := getObjects(g, "default", "app=test", client, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if sawLabel != "app=test" {
		t.Fatalf("LabelSelector = %q, want app=test", sawLabel)
	}
	if len(list.Items) != 1 {
		t.Fatalf("got %d items", len(list.Items))
	}
}

func TestGetObjects_imageStreamTags_getFailure(t *testing.T) {
	ist := &unstructured.Unstructured{}
	ist.SetAPIVersion("image.openshift.io/v1")
	ist.SetKind("ImageStreamTag")
	ist.SetNamespace("openshift")
	ist.SetName("ruby:latest")
	if err := unstructured.SetNestedField(ist.Object, map[string]interface{}{"tag": map[string]interface{}{}}, "spec"); err != nil {
		t.Fatal(err)
	}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), ist)
	client.PrependReactor("get", "imagestreamtags", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("get failed")
	})
	g := &groupResource{
		APIGroup:        "image.openshift.io",
		APIVersion:      "v1",
		APIGroupVersion: "image.openshift.io/v1",
		APIResource:     metav1.APIResource{Name: "imagestreamtags", Kind: "ImageStreamTag", Namespaced: true},
	}
	_, err := getObjects(g, "openshift", "", client, testLogger())
	if err == nil || !strings.Contains(err.Error(), "unable to process the list") {
		t.Fatalf("got err=%v", err)
	}
}

func TestIterateItemsInList_rejectsNonUnstructured(t *testing.T) {
	pod := &corev1.Pod{}
	pod.Name = "p1"
	list := &corev1.PodList{Items: []corev1.Pod{*pod}}
	g := &groupResource{APIGroup: "", APIResource: metav1.APIResource{Kind: "Pod"}}
	_, err := iterateItemsInList(list, g, testLogger())
	if err == nil || !strings.Contains(err.Error(), "unable to process the list") {
		t.Fatalf("got %v", err)
	}
}

func TestIterateItemsByGet_rejectsNonUnstructured(t *testing.T) {
	pod := &corev1.Pod{}
	pod.Name = "p1"
	list := &corev1.PodList{Items: []corev1.Pod{*pod}}
	g := &groupResource{APIGroup: "", APIResource: metav1.APIResource{Kind: "Pod"}}
	client := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	c := client.Resource(schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"})
	_, err := iterateItemsByGet(c, g, list, "default", testLogger())
	if err == nil || !strings.Contains(err.Error(), "unable to process the list") {
		t.Fatalf("got %v", err)
	}
}

// ---------- resourceToExtract integration ----------

func TestResourceToExtract_loadsConfigMaps(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cm1"},
		Data:       map[string]string{"k": "v"},
	}
	client := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme, cm)
	lists := []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: stdVerbs()},
			},
		},
	}
	resources, errs := resourceToExtract("default", "", client, lists, testLogger())
	if len(errs) != 0 {
		t.Fatalf("unexpected errs: %v", errs)
	}
	if len(resources) != 1 || resources[0].APIResource.Name != "configmaps" || len(resources[0].objects.Items) != 1 {
		t.Fatalf("got %d resources", len(resources))
	}
}

func TestResourceToExtract_loadsClusterRoles(t *testing.T) {
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "export-test-role"},
		Rules:      []rbacv1.PolicyRule{{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"pods"}}},
	}
	client := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme, cr)
	lists := []*metav1.APIResourceList{
		{
			GroupVersion: "rbac.authorization.k8s.io/v1",
			APIResources: []metav1.APIResource{
				{Name: "clusterroles", Kind: "ClusterRole", Namespaced: false, Verbs: stdVerbs()},
			},
		},
	}
	resources, errs := resourceToExtract("default", "", client, lists, testLogger())
	if len(errs) != 0 {
		t.Fatalf("unexpected errs: %v", errs)
	}
	if len(resources) != 1 || resources[0].objects.Items[0].GetName() != "export-test-role" {
		t.Fatalf("got %#v", resources)
	}
}

func TestResourceToExtract_listForbidden(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	client.PrependReactor("list", "configmaps", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, "", errors.New("no"))
	})
	lists := []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: stdVerbs()},
			},
		},
	}
	resources, errs := resourceToExtract("default", "", client, lists, testLogger())
	if len(resources) != 0 {
		t.Fatalf("expected no resources, got %d", len(resources))
	}
	if len(errs) != 1 || !apierrors.IsForbidden(errs[0].Error) {
		t.Fatalf("got errs %#v", errs)
	}
}

func TestResourceToExtract_listNotFound(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	client.PrependReactor("list", "configmaps", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "x")
	})
	lists := []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: stdVerbs()},
			},
		},
	}
	resources, errs := resourceToExtract("default", "", client, lists, testLogger())
	if len(resources) != 0 || len(errs) != 1 || !apierrors.IsNotFound(errs[0].Error) {
		t.Fatalf("resources=%d errs=%v", len(resources), errs)
	}
}

func TestResourceToExtract_listMethodNotSupported(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	client.PrependReactor("list", "configmaps", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, apierrors.NewMethodNotSupported(schema.GroupResource{Resource: "configmaps"}, "list")
	})
	lists := []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: stdVerbs()},
			},
		},
	}
	resources, errs := resourceToExtract("default", "", client, lists, testLogger())
	if len(resources) != 0 || len(errs) != 1 || !apierrors.IsMethodNotSupported(errs[0].Error) {
		t.Fatalf("resources=%d errs=%v", len(resources), errs)
	}
}

func TestResourceToExtract_listGenericError(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	client.PrependReactor("list", "configmaps", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, fmt.Errorf("upstream timeout")
	})
	lists := []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: stdVerbs()},
			},
		},
	}
	resources, errs := resourceToExtract("default", "", client, lists, testLogger())
	if len(resources) != 0 || len(errs) != 1 || errs[0].Error.Error() != "upstream timeout" {
		t.Fatalf("resources=%d errs=%v", len(resources), errs)
	}
}

func TestResourceToExtract_zeroObjectsNotAdded(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	lists := []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: stdVerbs()},
			},
		},
	}
	resources, errs := resourceToExtract("default", "", client, lists, testLogger())
	if len(resources) != 0 || len(errs) != 0 {
		t.Fatalf("empty list should skip resource (no error), got resources=%d errs=%v", len(resources), errs)
	}
}

func TestResourceToExtract_skipsUnparseableGroupVersion(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	lists := []*metav1.APIResourceList{
		{
			// More than one "/" makes schema.ParseGroupVersion fail (no "/" => treated as version-only).
			GroupVersion: "example.com/v1/extra",
			APIResources: []metav1.APIResource{
				{Name: "widgets", Kind: "Widget", Namespaced: true, Verbs: stdVerbs()},
			},
		},
	}
	resources, errs := resourceToExtract("default", "", client, lists, testLogger())
	if len(resources) != 0 || len(errs) != 0 {
		t.Fatalf("got resources %d errs %d", len(resources), len(errs))
	}
}

// ---------- helpers ----------

func namespacedObj(ns, name string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetNamespace(ns)
	u.SetName(name)
	return u
}

func clusterScopedObj(name string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetName(name)
	return u
}

// crdObj returns a cluster-scoped CRD-shaped object (empty namespace), matching collectRelatedCRDs output.
func crdObj(name string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetName(name)
	u.SetKind("CustomResourceDefinition")
	u.SetAPIVersion("apiextensions.k8s.io/v1")
	return u
}
