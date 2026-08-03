package export

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestImageResourceGuidance(t *testing.T) {
	tests := []struct {
		name     string
		apiGroup string
		kind     string
		wantOK   bool
	}{
		{name: "BuildConfig matches", apiGroup: "build.openshift.io", kind: "BuildConfig", wantOK: true},
		{name: "ImageStream matches", apiGroup: "image.openshift.io", kind: "ImageStream", wantOK: true},
		{name: "ImageStreamTag matches", apiGroup: "image.openshift.io", kind: "ImageStreamTag", wantOK: true},
		{name: "unrelated kind ConfigMap", apiGroup: "", kind: "ConfigMap", wantOK: false},
		{name: "unrelated kind Deployment", apiGroup: "apps", kind: "Deployment", wantOK: false},
		{name: "same Kind, different Group does not match", apiGroup: "something.else", kind: "ImageStream", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, ok := imageResourceGuidance(tt.apiGroup, tt.kind)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && msg == "" {
				t.Fatalf("expected non-empty guidance message when ok is true")
			}
		})
	}
}

func buildConfigGroupResource() *groupResource {
	return &groupResource{
		APIGroup:        "build.openshift.io",
		APIVersion:      "v1",
		APIGroupVersion: "build.openshift.io/v1",
		APIResource: metav1.APIResource{
			Name:       "buildconfigs",
			Kind:       "BuildConfig",
			Namespaced: true,
		},
		objects: &unstructured.UnstructuredList{
			Items: []unstructured.Unstructured{{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "my-build", "namespace": "myapp"}}}},
		},
	}
}

func imageStreamGroupResource() *groupResource {
	return &groupResource{
		APIGroup:        "image.openshift.io",
		APIVersion:      "v1",
		APIGroupVersion: "image.openshift.io/v1",
		APIResource: metav1.APIResource{
			Name:       "imagestreams",
			Kind:       "ImageStream",
			Namespaced: true,
		},
		objects: &unstructured.UnstructuredList{
			Items: []unstructured.Unstructured{{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "my-imagestream"}}}},
		},
	}
}

func TestWarnAboutImageResources(t *testing.T) {
	var buf bytes.Buffer
	log := logrus.New()
	log.SetOutput(&buf)
	log.SetLevel(logrus.WarnLevel)

	resources := []*groupResource{
		buildConfigGroupResource(),
		imageStreamGroupResource(),
		widgetGroupResource(), // unrelated resource; should not produce a warning
	}

	warnAboutImageResources(resources, log)

	output := buf.String()
	if !strings.Contains(output, "myapp/my-build") || !strings.Contains(output, "BuildConfig") {
		t.Errorf("expected warning naming namespaced BuildConfig %q, got: %s", "myapp/my-build", output)
	}
	if !strings.Contains(output, "my-imagestream") || !strings.Contains(output, "ImageStream") {
		t.Errorf("expected warning naming ImageStream %q, got: %s", "my-imagestream", output)
	}
	if strings.Contains(output, "Widget") || strings.Contains(output, "w1") {
		t.Errorf("did not expect a warning for unrelated Widget resource, got: %s", output)
	}
	if lines := strings.Count(strings.TrimSpace(output), "\n") + 1; lines != 2 {
		t.Errorf("expected exactly 2 warning lines, got %d: %s", lines, output)
	}
}
