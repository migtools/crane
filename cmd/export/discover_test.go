package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
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
