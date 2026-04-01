package export

import (
	"sort"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func TestShouldSkipCRDGroup_DefaultsAndOverrides(t *testing.T) {
	tests := []struct {
		name     string
		group    string
		includes []string
		skips    []string
		wantSkip bool
	}{
		{name: "default builtin skipped", group: "apps", wantSkip: true},
		{name: "default openshift suffix skipped", group: "route.openshift.io", wantSkip: true},
		{name: "custom group included by default", group: "example.com", wantSkip: false},
		{name: "user skip works", group: "acme.corp", skips: []string{"acme.corp"}, wantSkip: true},
		{name: "include overrides default builtin", group: "apps", includes: []string{"apps"}, wantSkip: false},
		{name: "include overrides openshift suffix", group: "route.openshift.io", includes: []string{"route.openshift.io"}, wantSkip: false},
		{name: "include overrides user skip", group: "acme.corp", includes: []string{"acme.corp"}, skips: []string{"acme.corp"}, wantSkip: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSkipCRDGroup(tt.group, normalizeGroupSet(tt.includes), normalizeGroupSet(tt.skips))
			if got != tt.wantSkip {
				t.Fatalf("shouldSkipCRDGroup(%q)=%v want %v", tt.group, got, tt.wantSkip)
			}
		})
	}
}

func crdUnstructured(name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("apiextensions.k8s.io/v1")
	u.SetKind("CustomResourceDefinition")
	u.SetName(name)
	return u
}

func widgetGroupResource() *groupResource {
	return &groupResource{
		APIGroup:        "example.com",
		APIVersion:      "v1",
		APIGroupVersion: "example.com/v1",
		APIResource: metav1.APIResource{
			Name:       "widgets",
			Kind:       "Widget",
			Namespaced: true,
		},
		objects: &unstructured.UnstructuredList{
			Items: []unstructured.Unstructured{{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "w1"}}}},
		},
	}
}

func TestCollectRelatedCRDs_customResourceOneCRD(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(scheme, crdUnstructured("widgets.example.com"))
	log := testLogger()

	got, errs := collectRelatedCRDs([]*groupResource{widgetGroupResource()}, client, log, nil, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].APIResource.Kind != "CustomResourceDefinition" {
		t.Fatalf("Kind = %q", got[0].APIResource.Kind)
	}
	if len(got[0].objects.Items) != 1 || got[0].objects.Items[0].GetName() != "widgets.example.com" {
		t.Fatalf("unexpected CRD object: %#v", got[0].objects.Items)
	}
}

func TestCollectRelatedCRDs_builtinGroupNoFetch(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(scheme)
	log := testLogger()

	gr := &groupResource{
		APIGroup:        "apps",
		APIVersion:      "v1",
		APIGroupVersion: "apps/v1",
		APIResource: metav1.APIResource{
			Name:       "deployments",
			Kind:       "Deployment",
			Namespaced: true,
		},
		objects: &unstructured.UnstructuredList{
			Items: []unstructured.Unstructured{{}},
		},
	}
	got, errs := collectRelatedCRDs([]*groupResource{gr}, client, log, nil, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 0 {
		t.Fatalf("expected no CRDs, got %d", len(got))
	}
}

func TestCollectRelatedCRDs_dedupePluralGroup(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(scheme, crdUnstructured("widgets.example.com"))
	log := testLogger()

	w1 := widgetGroupResource()
	w2 := widgetGroupResource()
	w2.objects.Items[0].SetName("w2")

	got, errs := collectRelatedCRDs([]*groupResource{w1, w2}, client, log, nil, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (deduped)", len(got))
	}
}

func TestCollectRelatedCRDs_skipsSubresourceName(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(scheme, crdUnstructured("widgets.example.com"))
	log := testLogger()

	gr := widgetGroupResource()
	gr.APIResource.Name = "widgets/status"

	got, errs := collectRelatedCRDs([]*groupResource{gr}, client, log, nil, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 0 {
		t.Fatalf("expected subresource skipped, got %d CRD rows", len(got))
	}
}

func TestCollectRelatedCRDs_multipleDistinctCRDs(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(scheme,
		crdUnstructured("widgets.example.com"),
		crdUnstructured("gadgets.other.example.com"),
	)
	log := testLogger()

	gadget := &groupResource{
		APIGroup:        "other.example.com",
		APIVersion:      "v1",
		APIGroupVersion: "other.example.com/v1",
		APIResource: metav1.APIResource{
			Name:       "gadgets",
			Kind:       "Gadget",
			Namespaced: true,
		},
		objects: &unstructured.UnstructuredList{
			Items: []unstructured.Unstructured{{}},
		},
	}

	got, errs := collectRelatedCRDs([]*groupResource{widgetGroupResource(), gadget}, client, log, nil, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	names := []string{got[0].objects.Items[0].GetName(), got[1].objects.Items[0].GetName()}
	sort.Strings(names)
	if names[0] != "gadgets.other.example.com" || names[1] != "widgets.example.com" {
		t.Fatalf("names = %v", names)
	}
}

func TestCollectRelatedCRDs_getFailureReturnsGroupResourceError(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(scheme)
	log := testLogger()

	got, errs := collectRelatedCRDs([]*groupResource{widgetGroupResource()}, client, log, nil, nil)
	if len(got) != 0 {
		t.Fatalf("expected no CRD rows, got %d", len(got))
	}
	if len(errs) != 1 {
		t.Fatalf("len(errs) = %d, want 1", len(errs))
	}
	if errs[0].APIResource.Name != "customresourcedefinition-widgets.example.com" {
		t.Fatalf("APIResource.Name = %q", errs[0].APIResource.Name)
	}
	if errs[0].APIResource.Kind != "CustomResourceDefinition" {
		t.Fatalf("Kind = %q", errs[0].APIResource.Kind)
	}
	if !apierrors.IsNotFound(errs[0].Error) {
		t.Fatalf("expected NotFound, got %v", errs[0].Error)
	}
}

func TestCollectRelatedCRDs_IncludeOverridesBuiltinGroup(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(scheme, crdUnstructured("routes.route.openshift.io"))
	log := testLogger()

	gr := &groupResource{
		APIGroup:        "route.openshift.io",
		APIVersion:      "v1",
		APIGroupVersion: "route.openshift.io/v1",
		APIResource: metav1.APIResource{
			Name:       "routes",
			Kind:       "Route",
			Namespaced: true,
		},
		objects: &unstructured.UnstructuredList{
			Items: []unstructured.Unstructured{{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "r1"}}}},
		},
	}

	got, errs := collectRelatedCRDs([]*groupResource{gr}, client, log, nil, []string{"route.openshift.io"})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].objects.Items[0].GetName() != "routes.route.openshift.io" {
		t.Fatalf("unexpected CRD object: %s", got[0].objects.Items[0].GetName())
	}
}
