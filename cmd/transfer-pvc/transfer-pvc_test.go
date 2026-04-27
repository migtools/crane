package transfer_pvc

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	securityv1 "github.com/openshift/api/security/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

func TestGetRouteHostName_ShortPrefix(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = configv1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	namespacedName := types.NamespacedName{
		Name:      "short-name",
		Namespace: "short-ns",
	}

	hostname, err := getRouteHostName(fakeClient, namespacedName)
	if err != nil {
		t.Errorf("getRouteHostName() unexpected error = %v", err)
	}
	if hostname != nil {
		t.Errorf("getRouteHostName() expected nil hostname for short prefix, got %v", *hostname)
	}
}

func TestGetRouteHostName_LongPrefix(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = configv1.AddToScheme(scheme)

	ingressConfig := &configv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Spec: configv1.IngressSpec{
			Domain: "apps.example.com",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ingressConfig).Build()

	// Create a name+namespace that exceeds 62 chars (name + "-" + namespace > 62)
	longName := "this-is-a-very-long-pvc-name-that-exceeds-the-limit"
	longNamespace := "this-is-also-a-long-namespace"
	namespacedName := types.NamespacedName{
		Name:      longName,
		Namespace: longNamespace,
	}

	// Verify our test setup: prefix should exceed 62 chars
	prefix := longName + "-" + longNamespace
	if len(prefix) <= 62 {
		t.Fatalf("Test setup error: prefix %q has length %d, should be > 62", prefix, len(prefix))
	}

	hostname, err := getRouteHostName(fakeClient, namespacedName)
	if err != nil {
		t.Fatalf("getRouteHostName() unexpected error = %v", err)
	}
	if hostname == nil {
		t.Fatal("getRouteHostName() expected non-nil hostname for long prefix, got nil")
	}

	expectedHostname := prefix[:62] + ".apps.example.com"
	if *hostname != expectedHostname {
		t.Errorf("getRouteHostName() = %q, want %q", *hostname, expectedHostname)
	}
}
func TestGetRouteHostName_LongPrefix_IngressNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = configv1.AddToScheme(scheme)

	// No Ingress object in the fake client
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	longName := "this-is-a-very-long-pvc-name-that-exceeds-the-limit"
	longNamespace := "this-is-also-a-long-namespace"
	namespacedName := types.NamespacedName{
		Name:      longName,
		Namespace: longNamespace,
	}

	_, err := getRouteHostName(fakeClient, namespacedName)
	if err == nil {
		t.Error("getRouteHostName() expected error when Ingress not found, got nil")
	}
}

// Tests that running pods with matching PVC volumes return the correct node name.
// Cases: single PVC volume, multiple volumes with one matching.
func TestGetNodeNameForPVC_FindsPodWithMatchingVolume(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name         string
		namespace    string
		pvcName      string
		pods         []corev1.Pod
		wantNodeName string
		wantErr      bool
	}{
		{
			name:      "running pod with matching PVC volume returns node name",
			namespace: "test-ns",
			pvcName:   "my-pvc",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-pod",
						Namespace: "test-ns",
					},
					Spec: corev1.PodSpec{
						NodeName: "worker-node-1",
						Volumes: []corev1.Volume{
							{
								Name: "data",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "my-pvc",
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
			wantNodeName: "worker-node-1",
			wantErr:      false,
		},
		{
			name:      "multiple volumes, one matching",
			namespace: "test-ns",
			pvcName:   "target-pvc",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "multi-vol-pod",
						Namespace: "test-ns",
					},
					Spec: corev1.PodSpec{
						NodeName: "worker-node-2",
						Volumes: []corev1.Volume{
							{
								Name: "config",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: "config"},
									},
								},
							},
							{
								Name: "data",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "target-pvc",
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
			wantNodeName: "worker-node-2",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]client.Object, len(tt.pods))
			for i := range tt.pods {
				objects[i] = &tt.pods[i]
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			gotNodeName, err := getNodeNameForPVC(fakeClient, tt.namespace, tt.pvcName)
			if (err != nil) != tt.wantErr {
				t.Errorf("getNodeNameForPVC() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotNodeName != tt.wantNodeName {
				t.Errorf("getNodeNameForPVC() = %v, want %v", gotNodeName, tt.wantNodeName)
			}
		})
	}
}

// Tests that empty string is returned when no matching pod is found.
// Cases: no pods in namespace, pods without PVC volumes, pods with different PVC.
func TestGetNodeNameForPVC_EmptyResultWhenNoPodFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name         string
		namespace    string
		pvcName      string
		pods         []corev1.Pod
		wantNodeName string
	}{
		{
			name:         "no pods in namespace returns empty string",
			namespace:    "empty-ns",
			pvcName:      "my-pvc",
			pods:         []corev1.Pod{},
			wantNodeName: "",
		},
		{
			name:      "pods without PVC volumes returns empty string",
			namespace: "test-ns",
			pvcName:   "my-pvc",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "configmap-pod",
						Namespace: "test-ns",
					},
					Spec: corev1.PodSpec{
						NodeName: "worker-node-1",
						Volumes: []corev1.Volume{
							{
								Name: "config",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: "config"},
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
			wantNodeName: "",
		},
		{
			name:      "pods with different PVC returns empty string",
			namespace: "test-ns",
			pvcName:   "my-pvc",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-pvc-pod",
						Namespace: "test-ns",
					},
					Spec: corev1.PodSpec{
						NodeName: "worker-node-1",
						Volumes: []corev1.Volume{
							{
								Name: "data",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "other-pvc",
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
			wantNodeName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]client.Object, len(tt.pods))
			for i := range tt.pods {
				objects[i] = &tt.pods[i]
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			gotNodeName, err := getNodeNameForPVC(fakeClient, tt.namespace, tt.pvcName)
			if err != nil {
				t.Errorf("getNodeNameForPVC() unexpected error = %v", err)
				return
			}
			if gotNodeName != tt.wantNodeName {
				t.Errorf("getNodeNameForPVC() = %v, want %v", gotNodeName, tt.wantNodeName)
			}
		})
	}
}

// Tests that non-running pods are skipped when searching for PVC node.
// Cases: pending pod skipped, succeeded pod skipped, failed pod skipped,
// only running pod matched among multiple.
func TestGetNodeNameForPVC_SkipsNonRunningPods(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name         string
		namespace    string
		pvcName      string
		pods         []corev1.Pod
		wantNodeName string
	}{
		{
			name:      "pending pod with matching PVC is skipped",
			namespace: "test-ns",
			pvcName:   "my-pvc",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pending-pod",
						Namespace: "test-ns",
					},
					Spec: corev1.PodSpec{
						NodeName: "worker-node-1",
						Volumes: []corev1.Volume{
							{
								Name: "data",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "my-pvc",
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
					},
				},
			},
			wantNodeName: "",
		},
		{
			name:      "succeeded pod with matching PVC is skipped",
			namespace: "test-ns",
			pvcName:   "my-pvc",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "succeeded-pod",
						Namespace: "test-ns",
					},
					Spec: corev1.PodSpec{
						NodeName: "worker-node-1",
						Volumes: []corev1.Volume{
							{
								Name: "data",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "my-pvc",
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodSucceeded,
					},
				},
			},
			wantNodeName: "",
		},
		{
			name:      "failed pod with matching PVC is skipped",
			namespace: "test-ns",
			pvcName:   "my-pvc",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "failed-pod",
						Namespace: "test-ns",
					},
					Spec: corev1.PodSpec{
						NodeName: "worker-node-1",
						Volumes: []corev1.Volume{
							{
								Name: "data",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "my-pvc",
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodFailed,
					},
				},
			},
			wantNodeName: "",
		},
		{
			name:      "only running pod is matched among multiple pods",
			namespace: "test-ns",
			pvcName:   "my-pvc",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pending-pod",
						Namespace: "test-ns",
					},
					Spec: corev1.PodSpec{
						NodeName: "pending-node",
						Volumes: []corev1.Volume{
							{
								Name: "data",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "my-pvc",
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "running-pod",
						Namespace: "test-ns",
					},
					Spec: corev1.PodSpec{
						NodeName: "running-node",
						Volumes: []corev1.Volume{
							{
								Name: "data",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "my-pvc",
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
			wantNodeName: "running-node",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]client.Object, len(tt.pods))
			for i := range tt.pods {
				objects[i] = &tt.pods[i]
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			gotNodeName, err := getNodeNameForPVC(fakeClient, tt.namespace, tt.pvcName)
			if err != nil {
				t.Errorf("getNodeNameForPVC() unexpected error = %v", err)
				return
			}
			if gotNodeName != tt.wantNodeName {
				t.Errorf("getNodeNameForPVC() = %v, want %v", gotNodeName, tt.wantNodeName)
			}
		})
	}
}

func TestGetIDsForNamespace_ParsesUIDAnnotation(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Annotations: map[string]string{
				securityv1.UIDRangeAnnotation: "1000650000/10000",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns).Build()

	ctx, err := getIDsForNamespace(fakeClient, "test-ns")
	if err != nil {
		t.Fatalf("getIDsForNamespace() unexpected error = %v", err)
	}

	if ctx.RunAsUser == nil {
		t.Fatal("getIDsForNamespace() RunAsUser is nil, expected non-nil")
	}
	if *ctx.RunAsUser != 1000650000 {
		t.Errorf("getIDsForNamespace() RunAsUser = %d, want %d", *ctx.RunAsUser, 1000650000)
	}
}

func TestGetIDsForNamespace_ParsesGIDAnnotation(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Annotations: map[string]string{
				securityv1.SupplementalGroupsAnnotation: "1000650000/10000",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns).Build()

	ctx, err := getIDsForNamespace(fakeClient, "test-ns")
	if err != nil {
		t.Fatalf("getIDsForNamespace() unexpected error = %v", err)
	}

	if ctx.RunAsGroup == nil {
		t.Fatal("getIDsForNamespace() RunAsGroup is nil, expected non-nil")
	}
	if *ctx.RunAsGroup != 1000650000 {
		t.Errorf("getIDsForNamespace() RunAsGroup = %d, want %d", *ctx.RunAsGroup, 1000650000)
	}
}

func TestGetIDsForNamespace_NoAnnotationsReturnsEmpty(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-ns",
			Annotations: map[string]string{},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns).Build()

	ctx, err := getIDsForNamespace(fakeClient, "test-ns")
	if err != nil {
		t.Fatalf("getIDsForNamespace() unexpected error = %v", err)
	}

	if ctx.RunAsUser != nil {
		t.Errorf("getIDsForNamespace() RunAsUser = %d, want nil", *ctx.RunAsUser)
	}
	if ctx.RunAsGroup != nil {
		t.Errorf("getIDsForNamespace() RunAsGroup = %d, want nil", *ctx.RunAsGroup)
	}
}

func TestGetIDsForNamespace_ErrorOnMissingNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	_, err := getIDsForNamespace(fakeClient, "nonexistent-ns")
	if err == nil {
		t.Error("getIDsForNamespace() expected error for missing namespace, got nil")
	}
}
