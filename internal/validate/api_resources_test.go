package validate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAPIResourcesJSON_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api-resources.json")
	writeFile(t, path, `{
  "kind": "APIResourceList",
  "apiVersion": "v1",
  "resources": [
    {
      "name": "deployments",
      "namespaced": true,
      "group": "apps",
      "version": "v1",
      "kind": "Deployment"
    },
    {
      "name": "services",
      "namespaced": true,
      "version": "v1",
      "kind": "Service"
    },
    {
      "name": "namespaces",
      "namespaced": false,
      "version": "v1",
      "kind": "Namespace"
    }
  ]
}`)

	index, err := ParseAPIResourcesJSON(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// apps/v1 should have Deployment
	if kinds, ok := index["apps/v1"]; !ok {
		t.Fatal("expected apps/v1 in index")
	} else if _, ok := kinds["Deployment"]; !ok {
		t.Fatal("expected Deployment in apps/v1")
	}

	// v1 should have Service and Namespace
	if kinds, ok := index["v1"]; !ok {
		t.Fatal("expected v1 in index")
	} else {
		if _, ok := kinds["Service"]; !ok {
			t.Fatal("expected Service in v1")
		}
		if _, ok := kinds["Namespace"]; !ok {
			t.Fatal("expected Namespace in v1")
		}
	}
}

func TestParseAPIResourcesJSON_CoreResources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api-resources.json")
	writeFile(t, path, `{
  "kind": "APIResourceList",
  "apiVersion": "v1",
  "resources": [
    {
      "name": "pods",
      "namespaced": true,
      "version": "v1",
      "kind": "Pod"
    }
  ]
}`)

	index, err := ParseAPIResourcesJSON(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Core resources have no group, so groupVersion = "v1"
	if _, ok := index["v1"]["Pod"]; !ok {
		t.Fatal("expected Pod under groupVersion v1 (core resource)")
	}
}

func TestParseAPIResourcesJSON_NonCoreResources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api-resources.json")
	writeFile(t, path, `{
  "kind": "APIResourceList",
  "apiVersion": "v1",
  "resources": [
    {
      "name": "routes",
      "namespaced": true,
      "group": "route.openshift.io",
      "version": "v1",
      "kind": "Route"
    }
  ]
}`)

	index, err := ParseAPIResourcesJSON(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := index["route.openshift.io/v1"]["Route"]; !ok {
		t.Fatal("expected Route under groupVersion route.openshift.io/v1")
	}
}

func TestParseAPIResourcesJSON_ResourcePlural(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api-resources.json")
	writeFile(t, path, `{
  "kind": "APIResourceList",
  "apiVersion": "v1",
  "resources": [
    {
      "name": "deployments",
      "namespaced": true,
      "group": "apps",
      "version": "v1",
      "kind": "Deployment"
    }
  ]
}`)

	index, err := ParseAPIResourcesJSON(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	de := index["apps/v1"]["Deployment"]
	if de.Resource.Name != "deployments" {
		t.Fatalf("Resource.Name = %q, want %q", de.Resource.Name, "deployments")
	}
	if !de.Resource.Namespaced {
		t.Fatal("expected Deployment to be namespaced")
	}
}

func TestParseAPIResourcesJSON_EmptyResources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api-resources.json")
	writeFile(t, path, `{
  "kind": "APIResourceList",
  "apiVersion": "v1",
  "resources": []
}`)

	_, err := ParseAPIResourcesJSON(path)
	if err == nil {
		t.Fatal("expected error for empty resources, got nil")
	}
}

func TestParseAPIResourcesJSON_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api-resources.json")
	writeFile(t, path, `{not valid json`)

	_, err := ParseAPIResourcesJSON(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestParseAPIResourcesJSON_FileNotFound(t *testing.T) {
	_, err := ParseAPIResourcesJSON("/nonexistent/file.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestParseAPIResourcesJSON_DuplicateKindAcrossGroups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api-resources.json")
	// Event exists in both v1 and events.k8s.io/v1
	writeFile(t, path, `{
  "kind": "APIResourceList",
  "apiVersion": "v1",
  "resources": [
    {
      "name": "events",
      "namespaced": true,
      "version": "v1",
      "kind": "Event"
    },
    {
      "name": "events",
      "namespaced": true,
      "group": "events.k8s.io",
      "version": "v1",
      "kind": "Event"
    }
  ]
}`)

	index, err := ParseAPIResourcesJSON(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := index["v1"]["Event"]; !ok {
		t.Fatal("expected Event in v1")
	}
	if _, ok := index["events.k8s.io/v1"]["Event"]; !ok {
		t.Fatal("expected Event in events.k8s.io/v1")
	}
}

func TestParseAPIResourcesJSON_VerifyNamespaced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api-resources.json")
	writeFile(t, path, `{
  "kind": "APIResourceList",
  "apiVersion": "v1",
  "resources": [
    {
      "name": "namespaces",
      "namespaced": false,
      "version": "v1",
      "kind": "Namespace"
    },
    {
      "name": "pods",
      "namespaced": true,
      "version": "v1",
      "kind": "Pod"
    }
  ]
}`)

	index, err := ParseAPIResourcesJSON(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if index["v1"]["Namespace"].Resource.Namespaced {
		t.Fatal("expected Namespace to be cluster-scoped (namespaced=false)")
	}
	if !index["v1"]["Pod"].Resource.Namespaced {
		t.Fatal("expected Pod to be namespaced (namespaced=true)")
	}
}

func writeAPIResourcesFile(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "api-resources.json")
	content := `{
  "kind": "APIResourceList",
  "apiVersion": "v1",
  "resources": [
    {"name": "deployments", "namespaced": true, "group": "apps", "version": "v1", "kind": "Deployment"},
    {"name": "services", "namespaced": true, "version": "v1", "kind": "Service"},
    {"name": "configmaps", "namespaced": true, "version": "v1", "kind": "ConfigMap"}
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
