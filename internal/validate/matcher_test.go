package validate

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakediscovery "k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"
)

func fakeDiscovery(resources []*metav1.APIResourceList) *fakediscovery.FakeDiscovery {
	fake := &fakediscovery.FakeDiscovery{
		Fake: &clienttesting.Fake{},
	}
	fake.Resources = resources
	return fake
}

func TestMatchResults_AllOK(t *testing.T) {
	disc := fakeDiscovery([]*metav1.APIResourceList{
		{
			GroupVersion: "apps/v1",
			APIResources: []metav1.APIResource{
				{Name: "deployments", Kind: "Deployment"},
			},
		},
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "configmaps", Kind: "ConfigMap"},
			},
		},
	})

	entries := []ManifestEntry{
		{APIVersion: "apps/v1", Kind: "Deployment", Group: "apps", Version: "v1", Namespace: "prod"},
		{APIVersion: "v1", Kind: "ConfigMap", Group: "", Version: "v1", Namespace: "prod"},
	}

	report, err := MatchResults(entries, MatchOptions{DiscoveryClient: disc}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Incompatible != 0 {
		t.Fatalf("Incompatible = %d, want 0", report.Incompatible)
	}
	if report.Compatible != 2 {
		t.Fatalf("Compatible = %d, want 2", report.Compatible)
	}
	for _, r := range report.Results {
		if r.Status != StatusOK {
			t.Fatalf("result %+v has status %s, want OK", r, r.Status)
		}
	}
}

func TestMatchResults_MissingGroupVersion(t *testing.T) {
	disc := fakeDiscovery([]*metav1.APIResourceList{
		{
			GroupVersion: "apps/v1",
			APIResources: []metav1.APIResource{
				{Name: "deployments", Kind: "Deployment"},
			},
		},
	})

	entries := []ManifestEntry{
		{APIVersion: "route.openshift.io/v1", Kind: "Route", Group: "route.openshift.io", Version: "v1", Namespace: "prod"},
	}

	report, err := MatchResults(entries, MatchOptions{DiscoveryClient: disc}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Incompatible != 1 {
		t.Fatalf("Incompatible = %d, want 1", report.Incompatible)
	}
	if report.Results[0].Reason == "" {
		t.Fatal("expected non-empty reason for incompatible result")
	}
}

func TestMatchResults_GroupVersionPresentKindMissing(t *testing.T) {
	disc := fakeDiscovery([]*metav1.APIResourceList{
		{
			GroupVersion: "apps/v1",
			APIResources: []metav1.APIResource{
				{Name: "deployments", Kind: "Deployment"},
			},
		},
	})

	entries := []ManifestEntry{
		{APIVersion: "apps/v1", Kind: "StatefulSet", Group: "apps", Version: "v1", Namespace: "prod"},
	}

	report, err := MatchResults(entries, MatchOptions{DiscoveryClient: disc}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Incompatible != 1 {
		t.Fatalf("Incompatible = %d, want 1", report.Incompatible)
	}
	r := report.Results[0]
	if r.Status != StatusIncompatible {
		t.Fatalf("status = %s, want Incompatible", r.Status)
	}
	if r.Reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestMatchResults_MixedResults(t *testing.T) {
	disc := fakeDiscovery([]*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "configmaps", Kind: "ConfigMap"},
			},
		},
	})

	entries := []ManifestEntry{
		{APIVersion: "v1", Kind: "ConfigMap", Group: "", Version: "v1", Namespace: "prod"},
		{APIVersion: "route.openshift.io/v1", Kind: "Route", Group: "route.openshift.io", Version: "v1", Namespace: "prod"},
	}

	report, err := MatchResults(entries, MatchOptions{DiscoveryClient: disc}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Compatible != 1 || report.Incompatible != 1 {
		t.Fatalf("Compatible=%d Incompatible=%d, want 1/1", report.Compatible, report.Incompatible)
	}
}

func TestMatchResults_SuggestionWhenAlternativeExists(t *testing.T) {
	disc := fakeDiscovery([]*metav1.APIResourceList{
		{
			GroupVersion: "apps/v1",
			APIResources: []metav1.APIResource{
				{Name: "deployments", Kind: "Deployment"},
			},
		},
	})

	entries := []ManifestEntry{
		{APIVersion: "extensions/v1beta1", Kind: "Deployment", Group: "extensions", Version: "v1beta1", Namespace: "prod"},
	}

	report, err := MatchResults(entries, MatchOptions{DiscoveryClient: disc}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Incompatible != 1 {
		t.Fatalf("Incompatible = %d, want 1", report.Incompatible)
	}
	r := report.Results[0]
	if r.Suggestion == "" {
		t.Fatal("expected non-empty suggestion when alternative GV exists")
	}
	if !strings.Contains(r.Suggestion, "apps/v1") {
		t.Fatalf("Suggestion = %q, expected it to mention apps/v1", r.Suggestion)
	}
	if !strings.Contains(r.Reason, "available as") {
		t.Fatalf("Reason = %q, expected it to contain suggestion hint", r.Reason)
	}
}

func TestMatchResults_NoSuggestionWhenKindNotOnTarget(t *testing.T) {
	disc := fakeDiscovery([]*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "configmaps", Kind: "ConfigMap"},
			},
		},
	})

	entries := []ManifestEntry{
		{APIVersion: "route.openshift.io/v1", Kind: "Route", Group: "route.openshift.io", Version: "v1", Namespace: "prod"},
	}

	report, err := MatchResults(entries, MatchOptions{DiscoveryClient: disc}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := report.Results[0]
	if r.Suggestion != "" {
		t.Fatalf("expected empty suggestion when kind is entirely absent, got %q", r.Suggestion)
	}
}

func TestMatchResults_EmptyEntries(t *testing.T) {
	disc := fakeDiscovery([]*metav1.APIResourceList{})

	report, err := MatchResults([]ManifestEntry{}, MatchOptions{DiscoveryClient: disc}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.TotalScanned != 0 {
		t.Fatalf("TotalScanned = %d, want 0", report.TotalScanned)
	}
	if report.HasIncompatible() {
		t.Fatal("empty report should not be incompatible")
	}
}

func TestMatchResults_ResourcePluralPopulated(t *testing.T) {
	disc := fakeDiscovery([]*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "services", Kind: "Service"},
			},
		},
	})

	entries := []ManifestEntry{
		{APIVersion: "v1", Kind: "Service", Group: "", Version: "v1", Namespace: "prod"},
	}

	report, err := MatchResults(entries, MatchOptions{DiscoveryClient: disc}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Results[0].ResourcePlural != "services" {
		t.Fatalf("ResourcePlural = %q, want %q", report.Results[0].ResourcePlural, "services")
	}
}
