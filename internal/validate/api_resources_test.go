package validate

import (
	"path/filepath"
	"testing"
)

func TestParseAPIResourcesJSON_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api-surface.json")
	writeFile(t, path, `{
  "apiResourceLists": [
    {
      "kind": "APIResourceList",
      "apiVersion": "v1",
      "groupVersion": "apps/v1",
      "resources": [
        {"name": "deployments", "namespaced": true, "kind": "Deployment", "verbs": ["get","list"]}
      ]
    },
    {
      "kind": "APIResourceList",
      "apiVersion": "v1",
      "groupVersion": "v1",
      "resources": [
        {"name": "services", "namespaced": true, "kind": "Service", "verbs": ["get","list"]},
        {"name": "namespaces", "namespaced": false, "kind": "Namespace", "verbs": ["get","list"]}
      ]
    }
  ]
}`)

	index, err := ParseAPIResourcesJSON(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if kinds, ok := index["apps/v1"]; !ok {
		t.Fatal("expected apps/v1 in index")
	} else if _, ok := kinds["Deployment"]; !ok {
		t.Fatal("expected Deployment in apps/v1")
	}

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
	path := filepath.Join(dir, "api-surface.json")
	writeFile(t, path, `{
  "apiResourceLists": [
    {
      "kind": "APIResourceList",
      "apiVersion": "v1",
      "groupVersion": "v1",
      "resources": [
        {"name": "pods", "namespaced": true, "kind": "Pod", "verbs": ["get","list"]}
      ]
    }
  ]
}`)

	index, err := ParseAPIResourcesJSON(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := index["v1"]["Pod"]; !ok {
		t.Fatal("expected Pod under groupVersion v1 (core resource)")
	}
}

func TestParseAPIResourcesJSON_NonCoreResources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api-surface.json")
	writeFile(t, path, `{
  "apiResourceLists": [
    {
      "kind": "APIResourceList",
      "apiVersion": "v1",
      "groupVersion": "route.openshift.io/v1",
      "resources": [
        {"name": "routes", "namespaced": true, "kind": "Route", "verbs": ["get","list"]}
      ]
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
	path := filepath.Join(dir, "api-surface.json")
	writeFile(t, path, `{
  "apiResourceLists": [
    {
      "kind": "APIResourceList",
      "apiVersion": "v1",
      "groupVersion": "apps/v1",
      "resources": [
        {"name": "deployments", "namespaced": true, "kind": "Deployment", "verbs": ["get","list"]}
      ]
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
	path := filepath.Join(dir, "api-surface.json")
	writeFile(t, path, `{"apiResourceLists": []}`)

	_, err := ParseAPIResourcesJSON(path)
	if err == nil {
		t.Fatal("expected error for empty api resource lists, got nil")
	}
}

func TestParseAPIResourcesJSON_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api-surface.json")
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
	path := filepath.Join(dir, "api-surface.json")
	writeFile(t, path, `{
  "apiResourceLists": [
    {
      "kind": "APIResourceList",
      "apiVersion": "v1",
      "groupVersion": "v1",
      "resources": [
        {"name": "events", "namespaced": true, "kind": "Event", "verbs": ["get","list"]}
      ]
    },
    {
      "kind": "APIResourceList",
      "apiVersion": "v1",
      "groupVersion": "events.k8s.io/v1",
      "resources": [
        {"name": "events", "namespaced": true, "kind": "Event", "verbs": ["get","list"]}
      ]
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
	path := filepath.Join(dir, "api-surface.json")
	writeFile(t, path, `{
  "apiResourceLists": [
    {
      "kind": "APIResourceList",
      "apiVersion": "v1",
      "groupVersion": "v1",
      "resources": [
        {"name": "namespaces", "namespaced": false, "kind": "Namespace", "verbs": ["get","list"]},
        {"name": "pods", "namespaced": true, "kind": "Pod", "verbs": ["get","list"]}
      ]
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

func TestParseAPIResourcesJSON_SubresourcesFiltered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api-surface.json")
	writeFile(t, path, `{
  "apiResourceLists": [
    {
      "kind": "APIResourceList",
      "apiVersion": "v1",
      "groupVersion": "apps/v1",
      "resources": [
        {"name": "deployments", "namespaced": true, "kind": "Deployment", "verbs": ["get","list"]},
        {"name": "deployments/status", "namespaced": true, "kind": "Deployment", "verbs": ["get","patch"]},
        {"name": "deployments/scale", "namespaced": true, "kind": "Scale", "verbs": ["get","patch"]}
      ]
    }
  ]
}`)

	index, err := ParseAPIResourcesJSON(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := index["apps/v1"]["Deployment"]; !ok {
		t.Fatal("expected Deployment in apps/v1")
	}
	if len(index["apps/v1"]) != 1 {
		t.Fatalf("expected 1 resource in apps/v1 (subresources filtered), got %d", len(index["apps/v1"]))
	}
}
