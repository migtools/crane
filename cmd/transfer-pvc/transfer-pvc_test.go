package transfer_pvc

import (
	"context"
	"strings"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Tests parsing of source:destination mapping format.
// Cases: single value, valid mapping, double colon, empty destination, empty both, empty string.
func Test_parseSourceDestinationMapping(t *testing.T) {
	tests := []struct {
		name            string
		mapping         string
		wantSource      string
		wantDestination string
		wantErr         bool
	}{
		{
			name:            "given a string with only source name, should return same values for both source and destination",
			mapping:         "validstring",
			wantSource:      "validstring",
			wantDestination: "validstring",
			wantErr:         false,
		},
		{
			name:            "given a string with a valid source to destination mapping, should return correct values for source and destination",
			mapping:         "source:destination",
			wantSource:      "source",
			wantDestination: "destination",
			wantErr:         false,
		},
		{
			name:            "given a string with invalid source to destination mapping, should return error",
			mapping:         "source::destination",
			wantSource:      "",
			wantDestination: "",
			wantErr:         true,
		},
		{
			name:            "given a string with empty destination name, should return error",
			mapping:         "source:",
			wantSource:      "",
			wantDestination: "",
			wantErr:         true,
		},
		{
			name:            "given a mapping with empty source and destination strings, should return error",
			mapping:         ":",
			wantSource:      "",
			wantDestination: "",
			wantErr:         true,
		},
		{
			name:            "given an empty string, should return error",
			mapping:         "",
			wantSource:      "",
			wantDestination: "",
			wantErr:         true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSource, gotDestination, err := parseSourceDestinationMapping(tt.mapping)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSourceDestinationMapping() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotSource != tt.wantSource {
				t.Errorf("parseSourceDestinationMapping() gotSource = %v, want %v", gotSource, tt.wantSource)
			}
			if gotDestination != tt.wantDestination {
				t.Errorf("parseSourceDestinationMapping() gotDestination = %v, want %v", gotDestination, tt.wantDestination)
			}
		})
	}
}

func TestDeleteResourcesIteratively_SuccessfulDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	labels := map[string]string{
		"app.kubernetes.io/name": "crane",
	}

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "test-ns",
			Labels:    labels,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(svc).Build()

	iterativeTypes := []client.Object{
		&corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: corev1.SchemeGroupVersion.Version,
			},
		},
	}

	err := deleteResourcesIteratively(fakeClient, iterativeTypes, labels, "test-ns")
	if err != nil {
		t.Fatalf("deleteResourcesIteratively() error = %v, want nil", err)
	}

	// Verify service was deleted
	svcList := &corev1.ServiceList{}
	listErr := fakeClient.List(context.TODO(), svcList, client.InNamespace("test-ns"), client.MatchingLabels(labels))
	if listErr != nil {
		t.Fatalf("Failed to list services: %v", listErr)
	}
	if len(svcList.Items) != 0 {
		t.Errorf("deleteResourcesIteratively() did not delete service, still have %d items", len(svcList.Items))
	}
}

func TestDeleteResourcesIteratively_MultipleResourceTypes(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	labels := map[string]string{
		"app.kubernetes.io/name": "crane",
	}

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "test-ns",
			Labels:    labels,
		},
	}

	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test-ns",
			Labels:    labels,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(svc, cm).Build()

	iterativeTypes := []client.Object{
		&corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: corev1.SchemeGroupVersion.Version,
			},
		},
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: corev1.SchemeGroupVersion.Version,
			},
		},
	}

	err := deleteResourcesIteratively(fakeClient, iterativeTypes, labels, "test-ns")
	if err != nil {
		t.Fatalf("deleteResourcesIteratively() error = %v, want nil", err)
	}

	// Verify service was deleted
	svcList := &corev1.ServiceList{}
	if err := fakeClient.List(context.TODO(), svcList, client.InNamespace("test-ns"), client.MatchingLabels(labels)); err != nil {
		t.Fatalf("Failed to list services: %v", err)
	}
	if len(svcList.Items) != 0 {
		t.Errorf("deleteResourcesIteratively() did not delete service, still have %d items", len(svcList.Items))
	}

	// Verify configmap was deleted
	cmList := &corev1.ConfigMapList{}
	if err := fakeClient.List(context.TODO(), cmList, client.InNamespace("test-ns"), client.MatchingLabels(labels)); err != nil {
		t.Fatalf("Failed to list configmaps: %v", err)
	}
	if len(cmList.Items) != 0 {
		t.Errorf("deleteResourcesIteratively() did not delete configmap, still have %d items", len(cmList.Items))
	}
}
func TestDeleteResourcesIteratively_OnlyDeletesMatchingLabels(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	matchingLabels := map[string]string{
		"app.kubernetes.io/name": "crane",
	}

	nonMatchingLabels := map[string]string{
		"app.kubernetes.io/name": "other-app",
	}

	svcToDelete := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-to-delete",
			Namespace: "test-ns",
			Labels:    matchingLabels,
		},
	}

	svcToKeep := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-to-keep",
			Namespace: "test-ns",
			Labels:    nonMatchingLabels,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(svcToDelete, svcToKeep).Build()

	iterativeTypes := []client.Object{
		&corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: corev1.SchemeGroupVersion.Version,
			},
		},
	}

	err := deleteResourcesIteratively(fakeClient, iterativeTypes, matchingLabels, "test-ns")
	if err != nil {
		t.Fatalf("deleteResourcesIteratively() error = %v, want nil", err)
	}

	// Verify only matching service was deleted
	svcList := &corev1.ServiceList{}
	if err := fakeClient.List(context.TODO(), svcList, client.InNamespace("test-ns")); err != nil {
		t.Fatalf("Failed to list services: %v", err)
	}
	if len(svcList.Items) != 1 {
		t.Fatalf("Expected 1 service remaining, got %d", len(svcList.Items))
	}
	if svcList.Items[0].Name != "svc-to-keep" {
		t.Errorf("Wrong service remaining: got %s, want svc-to-keep", svcList.Items[0].Name)
	}
}

func TestDeleteResourcesIteratively_OnlyDeletesInNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	labels := map[string]string{
		"app.kubernetes.io/name": "crane",
	}

	svcInTargetNs := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-target-ns",
			Namespace: "target-ns",
			Labels:    labels,
		},
	}

	svcInOtherNs := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-other-ns",
			Namespace: "other-ns",
			Labels:    labels,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(svcInTargetNs, svcInOtherNs).Build()

	iterativeTypes := []client.Object{
		&corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: corev1.SchemeGroupVersion.Version,
			},
		},
	}

	err := deleteResourcesIteratively(fakeClient, iterativeTypes, labels, "target-ns")
	if err != nil {
		t.Fatalf("deleteResourcesIteratively() error = %v, want nil", err)
	}

	// Verify service in target namespace was deleted
	targetList := &corev1.ServiceList{}
	if err := fakeClient.List(context.TODO(), targetList, client.InNamespace("target-ns")); err != nil {
		t.Fatalf("Failed to list services in target-ns: %v", err)
	}
	if len(targetList.Items) != 0 {
		t.Errorf("Service in target-ns should be deleted, still have %d items", len(targetList.Items))
	}

	// Verify service in other namespace was NOT deleted
	otherList := &corev1.ServiceList{}
	if err := fakeClient.List(context.TODO(), otherList, client.InNamespace("other-ns")); err != nil {
		t.Fatalf("Failed to list services in other-ns: %v", err)
	}
	if len(otherList.Items) != 1 {
		t.Errorf("Service in other-ns should remain, got %d items", len(otherList.Items))
	}
}

func TestDeleteResourcesIteratively_NoResourcesExist(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	labels := map[string]string{
		"app.kubernetes.io/name": "crane",
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	iterativeTypes := []client.Object{
		&corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: corev1.SchemeGroupVersion.Version,
			},
		},
	}

	err := deleteResourcesIteratively(fakeClient, iterativeTypes, labels, "test-ns")
	if err != nil {
		t.Errorf("deleteResourcesIteratively() error = %v, want nil for empty namespace", err)
	}
}

func TestGarbageCollect_DeletesSourceClusterResources(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	labels := map[string]string{
		"app.kubernetes.io/name":          "crane",
		"app.kubernetes.io/component":     "transfer-pvc",
		"app.konveyor.io/created-for-pvc": "test-pvc",
	}

	srcPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "src-pod",
			Namespace: "src-ns",
			Labels:    labels,
		},
	}
	srcConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "src-cm",
			Namespace: "src-ns",
			Labels:    labels,
		},
	}
	srcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "src-secret",
			Namespace: "src-ns",
			Labels:    labels,
		},
	}

	srcClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(srcPod, srcConfigMap, srcSecret).
		Build()

	destClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	namespace := mappedNameVar{source: "src-ns", destination: "dest-ns"}

	err := garbageCollect(srcClient, destClient, labels, endpointNginx, namespace)
	if err != nil {
		t.Fatalf("garbageCollect() error = %v, want nil", err)
	}

	// Verify source resources were deleted
	podList := &corev1.PodList{}
	_ = srcClient.List(context.TODO(), podList, client.InNamespace("src-ns"), client.MatchingLabels(labels))
	if len(podList.Items) != 0 {
		t.Errorf("garbageCollect() did not delete source pods, still have %d", len(podList.Items))
	}

	cmList := &corev1.ConfigMapList{}
	_ = srcClient.List(context.TODO(), cmList, client.InNamespace("src-ns"), client.MatchingLabels(labels))
	if len(cmList.Items) != 0 {
		t.Errorf("garbageCollect() did not delete source configmaps, still have %d", len(cmList.Items))
	}

	secretList := &corev1.SecretList{}
	_ = srcClient.List(context.TODO(), secretList, client.InNamespace("src-ns"), client.MatchingLabels(labels))
	if len(secretList.Items) != 0 {
		t.Errorf("garbageCollect() did not delete source secrets, still have %d", len(secretList.Items))
	}
}

// Tests garbageCollect deletes Ingress from destination when endpoint is nginx
func TestGarbageCollect_DeletesDestClusterIngress(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	labels := map[string]string{
		"app.kubernetes.io/name":          "crane",
		"app.kubernetes.io/component":     "transfer-pvc",
		"app.konveyor.io/created-for-pvc": "test-pvc",
	}

	destIngress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dest-ingress",
			Namespace: "dest-ns",
			Labels:    labels,
		},
	}
	destPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dest-pod",
			Namespace: "dest-ns",
			Labels:    labels,
		},
	}

	srcClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	destClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(destIngress, destPod).
		Build()

	namespace := mappedNameVar{source: "src-ns", destination: "dest-ns"}

	err := garbageCollect(srcClient, destClient, labels, endpointNginx, namespace)
	if err != nil {
		t.Fatalf("garbageCollect() error = %v, want nil", err)
	}

	// Verify destination Ingress was deleted
	ingressList := &networkingv1.IngressList{}
	_ = destClient.List(context.TODO(), ingressList, client.InNamespace("dest-ns"), client.MatchingLabels(labels))
	if len(ingressList.Items) != 0 {
		t.Errorf("garbageCollect() did not delete destination ingress, still have %d", len(ingressList.Items))
	}

	// Verify destination Pod was deleted
	podList := &corev1.PodList{}
	_ = destClient.List(context.TODO(), podList, client.InNamespace("dest-ns"), client.MatchingLabels(labels))
	if len(podList.Items) != 0 {
		t.Errorf("garbageCollect() did not delete destination pods, still have %d", len(podList.Items))
	}
}

// Tests garbageCollect deletes Route from destination when endpoint is route
func TestGarbageCollect_DeletesDestClusterRoute(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = routev1.AddToScheme(scheme)

	labels := map[string]string{
		"app.kubernetes.io/name":          "crane",
		"app.kubernetes.io/component":     "transfer-pvc",
		"app.konveyor.io/created-for-pvc": "test-pvc",
	}

	destRoute := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dest-route",
			Namespace: "dest-ns",
			Labels:    labels,
		},
	}
	destPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dest-pod",
			Namespace: "dest-ns",
			Labels:    labels,
		},
	}

	srcClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	destClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(destRoute, destPod).
		Build()

	namespace := mappedNameVar{source: "src-ns", destination: "dest-ns"}

	err := garbageCollect(srcClient, destClient, labels, endpointRoute, namespace)
	if err != nil {
		t.Fatalf("garbageCollect() error = %v, want nil", err)
	}

	// Verify destination Route was deleted
	routeList := &routev1.RouteList{}
	_ = destClient.List(context.TODO(), routeList, client.InNamespace("dest-ns"), client.MatchingLabels(labels))
	if len(routeList.Items) != 0 {
		t.Errorf("garbageCollect() did not delete destination route, still have %d", len(routeList.Items))
	}

	// Verify destination Pod was deleted
	podList := &corev1.PodList{}
	_ = destClient.List(context.TODO(), podList, client.InNamespace("dest-ns"), client.MatchingLabels(labels))
	if len(podList.Items) != 0 {
		t.Errorf("garbageCollect() did not delete destination pods, still have %d", len(podList.Items))
	}
}

// Tests getValidatedResourceName returns original name if under 63 characters
func TestGetValidatedResourceName_ShortName(t *testing.T) {
	shortName := "my-pvc"
	got := getValidatedResourceName(shortName)
	if got != shortName {
		t.Errorf("getValidatedResourceName() = %v, want %v", got, shortName)
	}
}

// Tests getValidatedResourceName returns md5 hash prefixed with "crane-" for long names
func TestGetValidatedResourceName_LongName(t *testing.T) {
	longName := "this-is-a-very-long-persistent-volume-claim-name-that-exceeds-63-characters-limit"
	got := getValidatedResourceName(longName)

	if len(got) >= 63 {
		t.Errorf("getValidatedResourceName() returned name of length %d, want < 63", len(got))
	}
	if !strings.Contains(got, "crane-") {
		t.Errorf("getValidatedResourceName() = %v, expected to start with 'crane-'", got)
	}
}
