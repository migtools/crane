package validate

import (
	"bytes"
	"context"
	"strings"
	"testing"

	internalValidate "github.com/konveyor/crane/internal/validate"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestInspectTargetObjects(t *testing.T) {
	scheme := runtime.NewScheme()
	existing := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "app-config",
			"namespace": "my-app",
		},
	}}
	client := dynamicfake.NewSimpleDynamicClient(scheme, existing)
	client.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		getAction := action.(k8stesting.GetAction)
		if getAction.GetName() != "api" {
			return false, nil, nil
		}
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Group: "apps", Resource: "deployments"},
			getAction.GetName(),
			fmtError("forbidden"),
		)
	})

	entries := []internalValidate.ManifestEntry{
		{APIVersion: "v1", Kind: "ConfigMap", Namespace: "my-app", Names: []string{"app-config"}, Version: "v1"},
		{APIVersion: "v1", Kind: "Service", Namespace: "my-app", Names: []string{"api"}, Version: "v1"},
		{APIVersion: "apps/v1", Kind: "Deployment", Group: "apps", Namespace: "my-app", Names: []string{"api"}, Version: "v1"},
		{APIVersion: "route.openshift.io/v1", Kind: "Route", Group: "route.openshift.io", Namespace: "my-app", Names: []string{"api"}, Version: "v1"},
	}
	report := &internalValidate.ValidationReport{Results: []internalValidate.ValidationResult{
		{APIVersion: "v1", Kind: "ConfigMap", Namespace: "my-app", ResourcePlural: "configmaps", Status: internalValidate.StatusOK},
		{APIVersion: "v1", Kind: "Service", Namespace: "my-app", ResourcePlural: "services", Status: internalValidate.StatusOK},
		{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "my-app", ResourcePlural: "deployments", Status: internalValidate.StatusOK},
		{APIVersion: "route.openshift.io/v1", Kind: "Route", Namespace: "my-app", Status: internalValidate.StatusIncompatible},
	}}

	warnings := inspectTargetObjects(context.Background(), client, entries, report)
	if len(warnings.existing) != 1 || warnings.existing[0].name != "app-config" {
		t.Fatalf("existing = %+v, want ConfigMap/app-config", warnings.existing)
	}
	if len(warnings.unknown) != 1 || warnings.unknown[0].object.name != "api" {
		t.Fatalf("unknown = %+v, want forbidden Deployment/api", warnings.unknown)
	}

	var out bytes.Buffer
	formatTargetObjectWarnings(&out, warnings)
	got := out.String()
	for _, want := range []string{
		"Warning: 1 rendered resource(s) already exist on the target cluster.",
		"- ConfigMap/my-app/app-config",
		"Warning: 1 rendered resource(s) could not be inspected on the target cluster:",
		"- Deployment/my-app/api: deployments.apps \"api\" is forbidden",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("warning output missing %q:\\n%s", want, got)
		}
	}
}

type fmtError string

func (e fmtError) Error() string { return string(e) }
