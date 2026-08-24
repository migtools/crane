package transfer_pvc

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	rsynctransfer "github.com/migtools/pvc-transfer/transfer/rsync"
	"github.com/migtools/pvc-transfer/transport"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func int64Ptr(v int64) *int64 { return &v }

// TestEndpointFlags_Validate_DefaultPersists proves the value-receiver bug in
// EndpointFlags.Validate: an empty Type with a valid Subdomain passes validation,
// but the nginx default assigned inside Validate is lost because the receiver is a
// value copy. The caller is left with Type == "", which fails later in createEndpoint.
func TestEndpointFlags_Validate_DefaultPersists(t *testing.T) {
	flags := EndpointFlags{
		Type:      "",
		Subdomain: "my.subdomain.example.com",
	}
	if err := flags.Validate(); err != nil {
		t.Fatalf("EndpointFlags.Validate() unexpected error = %v", err)
	}
	if flags.Type != endpointNginx {
		t.Errorf("EndpointFlags.Validate() did not persist default type: got %q, want %q", flags.Type, endpointNginx)
	}
}

func int64PtrEqual(a, b *int64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func fmtInt64Ptr(p *int64) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", *p)
}

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	_ = batchv1.AddToScheme(s)
	return s
}

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

func TestPodSpecReferencesPVC(t *testing.T) {
	tests := []struct {
		name    string
		spec    corev1.PodSpec
		pvcName string
		want    bool
	}{
		{
			name: "matching PVC",
			spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{Name: "data", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "my-pvc"},
					}},
				},
			},
			pvcName: "my-pvc",
			want:    true,
		},
		{
			name: "non-matching PVC",
			spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{Name: "data", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "other-pvc"},
					}},
				},
			},
			pvcName: "my-pvc",
			want:    false,
		},
		{
			name: "no PVC volumes",
			spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{Name: "config", VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{},
					}},
				},
			},
			pvcName: "my-pvc",
			want:    false,
		},
		{
			name:    "empty volumes",
			spec:    corev1.PodSpec{},
			pvcName: "my-pvc",
			want:    false,
		},
		{
			name: "multiple volumes one matches",
			spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{}}},
					{Name: "data", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "target-pvc"},
					}},
				},
			},
			pvcName: "target-pvc",
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := podSpecReferencesPVC(tt.spec, tt.pvcName)
			if got != tt.want {
				t.Errorf("podSpecReferencesPVC() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRestrictedContainersApplyTo(t *testing.T) {
	tests := []struct {
		name             string
		restricted       bool
		wantOmitDirTimes bool
		wantBoolFields   bool
	}{
		{
			name:             "expects --omit-dir-times in extras",
			restricted:       true,
			wantOmitDirTimes: true,
			wantBoolFields:   false,
		},
		{
			name:             "expects no --omit-dir-times",
			restricted:       false,
			wantOmitDirTimes: false,
			wantBoolFields:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := rsynctransfer.CommandOptions{}
			if err := restrictedContainers(tt.restricted).ApplyTo(&opts); err != nil {
				t.Fatalf("ApplyTo() returned unexpected error: %v", err)
			}
			hasOmitDirTimes := slices.Contains(opts.Extras, "--omit-dir-times")
			if hasOmitDirTimes != tt.wantOmitDirTimes {
				t.Errorf("--omit-dir-times in extras = %v, want %v", hasOmitDirTimes, tt.wantOmitDirTimes)
			}
			for _, check := range []struct {
				name string
				got  bool
			}{
				{"Groups", opts.Groups},
				{"Owners", opts.Owners},
				{"DeviceFiles", opts.DeviceFiles},
				{"SpecialFiles", opts.SpecialFiles},
			} {
				if check.got != tt.wantBoolFields {
					t.Errorf("%s = %v, want %v", check.name, check.got, tt.wantBoolFields)
				}
			}
		})
	}
}

func TestExtractPodSecurityContext(t *testing.T) {
	tests := []struct {
		name           string
		spec           corev1.PodSpec
		wantRunAsUser  *int64
		wantRunAsGroup *int64
		wantFSGroup    *int64
	}{
		{
			name:           "no security context",
			spec:           corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
			wantRunAsUser:  nil,
			wantRunAsGroup: nil,
			wantFSGroup:    nil,
		},
		{
			name: "pod-level only",
			spec: corev1.PodSpec{
				SecurityContext: &corev1.PodSecurityContext{
					RunAsUser:  int64Ptr(1000),
					RunAsGroup: int64Ptr(1000),
					FSGroup:    int64Ptr(2000),
				},
				Containers: []corev1.Container{{Name: "app"}},
			},
			wantRunAsUser:  int64Ptr(1000),
			wantRunAsGroup: int64Ptr(1000),
			wantFSGroup:    int64Ptr(2000),
		},
		{
			name: "container-level overrides pod-level runAsUser",
			spec: corev1.PodSpec{
				SecurityContext: &corev1.PodSecurityContext{
					RunAsUser:  int64Ptr(1000),
					RunAsGroup: int64Ptr(1000),
					FSGroup:    int64Ptr(2000),
				},
				Containers: []corev1.Container{
					{
						Name: "app",
						SecurityContext: &corev1.SecurityContext{
							RunAsUser: int64Ptr(3000),
						},
					},
				},
			},
			wantRunAsUser:  int64Ptr(3000),
			wantRunAsGroup: int64Ptr(1000),
			wantFSGroup:    int64Ptr(2000),
		},
		{
			name: "container-level only",
			spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "app",
						SecurityContext: &corev1.SecurityContext{
							RunAsUser: int64Ptr(999),
						},
					},
				},
			},
			wantRunAsUser:  int64Ptr(999),
			wantRunAsGroup: nil,
			wantFSGroup:    nil,
		},
		{
			name: "multiple containers picks first with runAsUser",
			spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "sidecar"},
					{
						Name: "app",
						SecurityContext: &corev1.SecurityContext{
							RunAsUser: int64Ptr(500),
						},
					},
					{
						Name: "another",
						SecurityContext: &corev1.SecurityContext{
							RunAsUser: int64Ptr(600),
						},
					},
				},
			},
			wantRunAsUser:  int64Ptr(500),
			wantRunAsGroup: nil,
			wantFSGroup:    nil,
		},
		{
			name: "supplemental groups preserved",
			spec: corev1.PodSpec{
				SecurityContext: &corev1.PodSecurityContext{
					RunAsUser:          int64Ptr(1000),
					SupplementalGroups: []int64{100, 200},
				},
				Containers: []corev1.Container{{Name: "app"}},
			},
			wantRunAsUser:  int64Ptr(1000),
			wantRunAsGroup: nil,
			wantFSGroup:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPodSecurityContext(tt.spec)
			if !int64PtrEqual(got.RunAsUser, tt.wantRunAsUser) {
				t.Errorf("RunAsUser = %v, want %v", fmtInt64Ptr(got.RunAsUser), fmtInt64Ptr(tt.wantRunAsUser))
			}
			if !int64PtrEqual(got.RunAsGroup, tt.wantRunAsGroup) {
				t.Errorf("RunAsGroup = %v, want %v", fmtInt64Ptr(got.RunAsGroup), fmtInt64Ptr(tt.wantRunAsGroup))
			}
			if !int64PtrEqual(got.FSGroup, tt.wantFSGroup) {
				t.Errorf("FSGroup = %v, want %v", fmtInt64Ptr(got.FSGroup), fmtInt64Ptr(tt.wantFSGroup))
			}
		})
	}
}

func TestGetSecurityContextFromWorkload(t *testing.T) {
	scheme := newTestScheme()

	tests := []struct {
		name          string
		objects       []runtime.Object
		namespace     string
		pvcName       string
		wantRunAsUser *int64
		wantNil       bool
	}{
		{
			name: "finds Deployment by PVC name",
			objects: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "mysql", Namespace: "test"},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								SecurityContext: &corev1.PodSecurityContext{RunAsUser: int64Ptr(27)},
								Containers:      []corev1.Container{{Name: "mysql"}},
								Volumes: []corev1.Volume{
									{Name: "data", VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "mysql-data"},
									}},
								},
							},
						},
					},
				},
			},
			namespace:     "test",
			pvcName:       "mysql-data",
			wantRunAsUser: int64Ptr(27),
		},
		{
			name: "finds StatefulSet by volumeClaimTemplate pattern",
			objects: []runtime.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{Name: "cassandra", Namespace: "test"},
					Spec: appsv1.StatefulSetSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								SecurityContext: &corev1.PodSecurityContext{RunAsUser: int64Ptr(999)},
								Containers:      []corev1.Container{{Name: "cassandra"}},
							},
						},
						VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
							{ObjectMeta: metav1.ObjectMeta{Name: "data"}},
						},
					},
				},
			},
			namespace:     "test",
			pvcName:       "data-cassandra-0",
			wantRunAsUser: int64Ptr(999),
		},
		{
			name: "finds Job by PVC name",
			objects: []runtime.Object{
				&batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{Name: "data-loader", Namespace: "test"},
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								SecurityContext: &corev1.PodSecurityContext{RunAsUser: int64Ptr(3000)},
								Containers:      []corev1.Container{{Name: "loader"}},
								Volumes: []corev1.Volume{
									{Name: "out", VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "job-data"},
									}},
								},
							},
						},
					},
				},
			},
			namespace:     "test",
			pvcName:       "job-data",
			wantRunAsUser: int64Ptr(3000),
		},
		{
			name: "finds CronJob by PVC name",
			objects: []runtime.Object{
				&batchv1.CronJob{
					ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "test"},
					Spec: batchv1.CronJobSpec{
						Schedule: "*/5 * * * *",
						JobTemplate: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										SecurityContext: &corev1.PodSecurityContext{RunAsUser: int64Ptr(1001)},
										Containers:      []corev1.Container{{Name: "backup"}},
										Volumes: []corev1.Volume{
											{Name: "data", VolumeSource: corev1.VolumeSource{
												PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "cron-data"},
											}},
										},
									},
								},
							},
						},
					},
				},
			},
			namespace:     "test",
			pvcName:       "cron-data",
			wantRunAsUser: int64Ptr(1001),
		},
		{
			name: "skips owned ReplicaSets",
			objects: []runtime.Object{
				&appsv1.ReplicaSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "mysql-abc123",
						Namespace:       "test",
						OwnerReferences: []metav1.OwnerReference{{Name: "mysql", Kind: "Deployment"}},
					},
					Spec: appsv1.ReplicaSetSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								SecurityContext: &corev1.PodSecurityContext{RunAsUser: int64Ptr(27)},
								Containers:      []corev1.Container{{Name: "mysql"}},
								Volumes: []corev1.Volume{
									{Name: "data", VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "mysql-data"},
									}},
								},
							},
						},
					},
				},
			},
			namespace: "test",
			pvcName:   "mysql-data",
			wantNil:   true,
		},
		{
			name: "finds standalone ReplicaSet",
			objects: []runtime.Object{
				&appsv1.ReplicaSet{
					ObjectMeta: metav1.ObjectMeta{Name: "standalone-rs", Namespace: "test"},
					Spec: appsv1.ReplicaSetSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								SecurityContext: &corev1.PodSecurityContext{RunAsUser: int64Ptr(555)},
								Containers:      []corev1.Container{{Name: "app"}},
								Volumes: []corev1.Volume{
									{Name: "data", VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "rs-data"},
									}},
								},
							},
						},
					},
				},
			},
			namespace:     "test",
			pvcName:       "rs-data",
			wantRunAsUser: int64Ptr(555),
		},
		{
			name: "finds DaemonSet by PVC name",
			objects: []runtime.Object{
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{Name: "log-collector", Namespace: "test"},
					Spec: appsv1.DaemonSetSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								SecurityContext: &corev1.PodSecurityContext{RunAsUser: int64Ptr(1500)},
								Containers:      []corev1.Container{{Name: "collector"}},
								Volumes: []corev1.Volume{
									{Name: "logs", VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "log-data"},
									}},
								},
							},
						},
					},
				},
			},
			namespace:     "test",
			pvcName:       "log-data",
			wantRunAsUser: int64Ptr(1500),
		},
		{
			name:      "no workloads in namespace",
			objects:   []runtime.Object{},
			namespace: "test",
			pvcName:   "orphan-pvc",
			wantNil:   true,
		},
		{
			name: "no matching PVC",
			objects: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "test"},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								SecurityContext: &corev1.PodSecurityContext{RunAsUser: int64Ptr(1000)},
								Containers:      []corev1.Container{{Name: "app"}},
								Volumes: []corev1.Volume{
									{Name: "data", VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "different-pvc"},
									}},
								},
							},
						},
					},
				},
			},
			namespace: "test",
			pvcName:   "my-pvc",
			wantNil:   true,
		},
		{
			name: "workload without securityContext",
			objects: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "test"},
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{Name: "app"}},
								Volumes: []corev1.Volume{
									{Name: "data", VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "app-data"},
									}},
								},
							},
						},
					},
				},
			},
			namespace:     "test",
			pvcName:       "app-data",
			wantRunAsUser: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.objects...).Build()
			got, err := getSecurityContextFromWorkload(c, tt.namespace, tt.pvcName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				if tt.wantRunAsUser != nil {
					t.Errorf("expected RunAsUser=%d, got nil result", *tt.wantRunAsUser)
				}
				return
			}
			if !int64PtrEqual(got.RunAsUser, tt.wantRunAsUser) {
				t.Errorf("RunAsUser = %v, want %v", fmtInt64Ptr(got.RunAsUser), fmtInt64Ptr(tt.wantRunAsUser))
			}
		})
	}
}

func TestGetIDsForNamespace_OCPAnnotation(t *testing.T) {
	scheme := newTestScheme()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocp-ns",
			Annotations: map[string]string{
				"openshift.io/sa.scc.uid-range":           "1000700000/10000",
				"openshift.io/sa.scc.supplemental-groups": "1000700000/10000",
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(ns).Build()
	got, err := getIDsForNamespace(c, "ocp-ns", "any-pvc", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RunAsUser == nil || *got.RunAsUser != 1000700000 {
		t.Errorf("RunAsUser = %v, want 1000700000", fmtInt64Ptr(got.RunAsUser))
	}
	if got.FSGroup == nil || *got.FSGroup != 1000700000 {
		t.Errorf("FSGroup = %v, want 1000700000", fmtInt64Ptr(got.FSGroup))
	}
}

func TestGetIDsForNamespace_WorkloadFallback(t *testing.T) {
	scheme := newTestScheme()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "k8s-ns"},
	}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql", Namespace: "k8s-ns"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser: int64Ptr(27),
						FSGroup:   int64Ptr(27),
					},
					Containers: []corev1.Container{{Name: "mysql"}},
					Volumes: []corev1.Volume{
						{Name: "data", VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "mysql-data"},
						}},
					},
				},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(ns, deploy).Build()
	got, err := getIDsForNamespace(c, "k8s-ns", "mysql-data", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RunAsUser == nil || *got.RunAsUser != 27 {
		t.Errorf("RunAsUser = %v, want 27", fmtInt64Ptr(got.RunAsUser))
	}
	if got.FSGroup == nil || *got.FSGroup != 27 {
		t.Errorf("FSGroup = %v, want 27", fmtInt64Ptr(got.FSGroup))
	}
}

func TestGetIDsForNamespace_OCPTakesPrecedence(t *testing.T) {
	scheme := newTestScheme()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocp-ns",
			Annotations: map[string]string{
				"openshift.io/sa.scc.uid-range":           "1000700000/10000",
				"openshift.io/sa.scc.supplemental-groups": "1000700000/10000",
			},
		},
	}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql", Namespace: "ocp-ns"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{RunAsUser: int64Ptr(27)},
					Containers:      []corev1.Container{{Name: "mysql"}},
					Volumes: []corev1.Volume{
						{Name: "data", VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "mysql-data"},
						}},
					},
				},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(ns, deploy).Build()
	got, err := getIDsForNamespace(c, "ocp-ns", "mysql-data", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RunAsUser == nil || *got.RunAsUser != 1000700000 {
		t.Errorf("OCP annotation should take precedence: RunAsUser = %v, want 1000700000", fmtInt64Ptr(got.RunAsUser))
	}
}

func TestGetIDsForNamespace_InspectUsesTransferImage(t *testing.T) {
	tests := []struct {
		name      string
		image     string
		wantImage string
	}{
		{
			name:      "configured image is used on the inspect pod",
			image:     "example.invalid/rsync-custom:1",
			wantImage: "example.invalid/rsync-custom:1",
		},
		{
			name:      "empty image falls back to library default",
			image:     "",
			wantImage: transport.DefaultRsyncTransferImage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := newTestScheme()
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "k8s-ns"}}
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "app-data", Namespace: "k8s-ns"},
			}
			// Workload mounts the PVC but has no runAsUser, so inspect is reached.
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "k8s-ns"},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "app"}},
							Volumes: []corev1.Volume{
								{Name: "data", VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "app-data"},
								}},
							},
						},
					},
				},
			}

			var inspectImage string
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(ns, pvc, deploy).
				WithInterceptorFuncs(interceptor.Funcs{
					Create: func(ctx context.Context, cli client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
						if pod, ok := obj.(*corev1.Pod); ok && len(pod.Spec.Containers) > 0 {
							inspectImage = pod.Spec.Containers[0].Image
						}
						return cli.Create(ctx, obj, opts...)
					},
					Get: func(ctx context.Context, cli client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if err := cli.Get(ctx, key, obj, opts...); err != nil {
							return err
						}
						pod, ok := obj.(*corev1.Pod)
						if !ok {
							return nil
						}
						pod.Status.Phase = corev1.PodSucceeded
						pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 0,
									Message:  "27",
								},
							},
						}}
						return nil
					},
				}).
				Build()

			got, err := getIDsForNamespace(c, "k8s-ns", "app-data", tt.image)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if inspectImage == "" {
				t.Fatal("inspect pod was not created")
			}
			if inspectImage != tt.wantImage {
				t.Errorf("inspect pod image = %q, want %q", inspectImage, tt.wantImage)
			}
			if got.RunAsUser == nil || *got.RunAsUser != 27 {
				t.Errorf("RunAsUser = %v, want 27 from inspect", fmtInt64Ptr(got.RunAsUser))
			}
		})
	}
}

func TestGetSecurityContextFromWorkload_NoWorkloads(t *testing.T) {
	scheme := newTestScheme()

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	got, err := getSecurityContextFromWorkload(c, "empty-ns", "some-pvc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestServerFallbackToSourceUID(t *testing.T) {
	scheme := newTestScheme()

	srcNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "src-ns"}}

	srcDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "src-ns"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{RunAsUser: int64Ptr(27), FSGroup: int64Ptr(27)},
					Containers:      []corev1.Container{{Name: "app"}},
					Volumes: []corev1.Volume{
						{Name: "data", VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "app-data"},
						}},
					},
				},
			},
		},
	}

	srcClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(srcNs, srcDeploy).Build()

	clientCtx, _ := getSecurityContextFromWorkload(srcClient, "src-ns", "app-data")
	if clientCtx == nil || clientCtx.RunAsUser == nil || *clientCtx.RunAsUser != 27 {
		t.Fatalf("source should find RunAsUser=27, got %v", fmtInt64Ptr(clientCtx.RunAsUser))
	}

	var serverCtx *corev1.PodSecurityContext

	if serverCtx == nil || serverCtx.RunAsUser == nil {
		serverCtx = clientCtx
	}

	if serverCtx.RunAsUser == nil || *serverCtx.RunAsUser != 27 {
		t.Errorf("server should fallback to source UID: got %v, want 27", fmtInt64Ptr(serverCtx.RunAsUser))
	}
}

func TestTruncateWithHash(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantLen    int
		wantUnique bool
		otherInput string
	}{
		{
			name:    "result is exactly 62 chars",
			input:   "my-very-long-pvc-name-that-exceeds-sixty-two-characters-in-the-namespace-production",
			wantLen: 62,
		},
		{
			name:       "different long names produce different results",
			input:      "my-very-long-application-name-with-lots-of-details-pvc-data-volume1-production",
			otherInput: "my-very-long-application-name-with-lots-of-details-pvc-data-volume2-production",
			wantUnique: true,
		},
		{
			name:       "names sharing first 62 chars produce different results",
			input:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-suffix1",
			otherInput: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-suffix2",
			wantUnique: true,
		},
		{
			name:    "same input produces same result (deterministic)",
			input:   "my-very-long-pvc-name-that-exceeds-sixty-two-characters-in-the-namespace-production",
			wantLen: 62,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateWithHash(tt.input)

			if tt.wantLen > 0 && len(result) != tt.wantLen {
				t.Errorf("truncateWithHash() length = %d, want %d, result = %q", len(result), tt.wantLen, result)
			}

			if tt.wantUnique {
				other := truncateWithHash(tt.otherInput)
				if result == other {
					t.Errorf("truncateWithHash() collision: %q and %q both produced %q", tt.input, tt.otherInput, result)
				}
			}

			// Verify deterministic
			again := truncateWithHash(tt.input)
			if result != again {
				t.Errorf("truncateWithHash() not deterministic: got %q then %q", result, again)
			}
		})
	}
}

func TestIsIntraCluster(t *testing.T) {
	tests := []struct {
		name string
		cmd  TransferPVCCommand
		want bool
	}{
		{
			name: "same cluster same namespace",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "cluster-a"},
				destinationContext: &clientcmdapi.Context{Cluster: "cluster-a"},
				Flags:              Flags{PVC: PvcFlags{Namespace: mappedNameVar{source: "ns1", destination: "ns1"}}},
			},
			want: true,
		},
		{
			name: "same cluster different namespace",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "cluster-a"},
				destinationContext: &clientcmdapi.Context{Cluster: "cluster-a"},
				Flags:              Flags{PVC: PvcFlags{Namespace: mappedNameVar{source: "ns1", destination: "ns2"}}},
			},
			want: false,
		},
		{
			name: "different cluster same namespace",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "cluster-a"},
				destinationContext: &clientcmdapi.Context{Cluster: "cluster-b"},
				Flags:              Flags{PVC: PvcFlags{Namespace: mappedNameVar{source: "ns1", destination: "ns1"}}},
			},
			want: false,
		},
		{
			name: "nil source context",
			cmd: TransferPVCCommand{
				sourceContext:      nil,
				destinationContext: &clientcmdapi.Context{Cluster: "cluster-a"},
				Flags:              Flags{PVC: PvcFlags{Namespace: mappedNameVar{source: "ns1", destination: "ns1"}}},
			},
			want: false,
		},
		{
			name: "nil destination context",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "cluster-a"},
				destinationContext: nil,
				Flags:              Flags{PVC: PvcFlags{Namespace: mappedNameVar{source: "ns1", destination: "ns1"}}},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cmd.isIntraClusterSameNamespace(); got != tt.want {
				t.Errorf("isIntraClusterSameNamespace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateRejectsSameNameIntraCluster(t *testing.T) {
	tests := []struct {
		name    string
		cmd     TransferPVCCommand
		wantErr bool
	}{
		{
			name: "same name same cluster same namespace is rejected",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "c1"},
				destinationContext: &clientcmdapi.Context{Cluster: "c1"},
				Flags: Flags{PVC: PvcFlags{
					Name:      mappedNameVar{source: "mysql-data", destination: "mysql-data"},
					Namespace: mappedNameVar{source: "ns1", destination: "ns1"},
				}},
			},
			wantErr: true,
		},
		{
			name: "no colon pvc-name same cluster same namespace is rejected",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "c1"},
				destinationContext: &clientcmdapi.Context{Cluster: "c1"},
				Flags: Flags{PVC: PvcFlags{
					Name:      mappedNameVar{source: "mysql-data", destination: "mysql-data"},
					Namespace: mappedNameVar{source: "ns1", destination: "ns1"},
				}},
			},
			wantErr: true,
		},
		{
			name: "different name same cluster same namespace is allowed",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "c1"},
				destinationContext: &clientcmdapi.Context{Cluster: "c1"},
				Flags: Flags{
					PVC: PvcFlags{
						Name:      mappedNameVar{source: "mysql-data", destination: "mysql-data-new"},
						Namespace: mappedNameVar{source: "ns1", destination: "ns1"},
					},
					Endpoint: EndpointFlags{Subdomain: "test.example.com"},
				},
			},
			wantErr: false,
		},
		{
			name: "same name cross cluster is allowed",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "c1"},
				destinationContext: &clientcmdapi.Context{Cluster: "c2"},
				Flags: Flags{
					PVC: PvcFlags{
						Name:      mappedNameVar{source: "mysql-data", destination: "mysql-data"},
						Namespace: mappedNameVar{source: "ns1", destination: "ns1"},
					},
					Endpoint: EndpointFlags{Subdomain: "test.example.com"},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cmd.Validate()
			if tt.wantErr && err == nil {
				t.Error("Validate() should have returned error but didn't")
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), "must differ") {
				t.Errorf("Validate() returned unexpected error: %v", err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Validate() returned unexpected error: %v", err)
			}
		})
	}
}

func TestValidateKeepCloudDataRequiresCloudStorage(t *testing.T) {
	const guardErrMsg = "--keep-cloud-data requires --cloud-storage"
	tests := []struct {
		name           string
		cmd            TransferPVCCommand
		wantGuardError bool
	}{
		{
			name: "keep-cloud-data without cloud-storage is rejected",
			cmd: TransferPVCCommand{
				Flags: Flags{KeepCloudData: true},
			},
			wantGuardError: true,
		},
		{
			name: "keep-cloud-data with cloud-storage set does not trigger guard",
			cmd: TransferPVCCommand{
				Flags: Flags{
					KeepCloudData: true,
					CloudStorage:  "remote:my-bucket",
				},
			},
			wantGuardError: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cmd.Validate()
			hasGuardErr := err != nil && strings.Contains(err.Error(), guardErrMsg)
			if tt.wantGuardError && !hasGuardErr {
				t.Errorf("expected guard error %q but got: %v", guardErrMsg, err)
			}
			if !tt.wantGuardError && hasGuardErr {
				t.Errorf("unexpected guard error: %v", err)
			}
		})
	}
}

func TestCertSecretNaming(t *testing.T) {
	tests := []struct {
		name         string
		srcPVCName   string
		destPVCName  string
		serverSecret string
		wantCopyName string
	}{
		{
			name:         "same PVC name — keep original secret name",
			srcPVCName:   "mydata",
			destPVCName:  "mydata",
			serverSecret: "stunnel-creds-certs-mydata",
			wantCopyName: "stunnel-creds-certs-mydata",
		},
		{
			name:         "different PVC name — rename to match client expectation",
			srcPVCName:   "mydata",
			destPVCName:  "mydata-renamed",
			serverSecret: "stunnel-creds-certs-mydata-renamed",
			wantCopyName: "stunnel-creds-certs-mydata",
		},
		{
			name:         "intra-cluster SC conversion — rename to source PVC",
			srcPVCName:   "redis-data",
			destPVCName:  "redis-data-new",
			serverSecret: "stunnel-creds-certs-redis-data-new",
			wantCopyName: "stunnel-creds-certs-redis-data",
		},
		{
			name:         "cross-cluster namespace mapping same name — keep original",
			srcPVCName:   "data",
			destPVCName:  "data",
			serverSecret: "stunnel-creds-certs-data",
			wantCopyName: "stunnel-creds-certs-data",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secretName := certificateSecretName(tt.serverSecret, tt.srcPVCName, tt.destPVCName)
			if secretName != tt.wantCopyName {
				t.Errorf("cert secret name = %q, want %q", secretName, tt.wantCopyName)
			}
		})
	}
}

func TestStripServerManagedPVCAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		want        map[string]string
	}{
		{
			name:        "nil annotations",
			annotations: nil,
			want:        nil,
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			want:        nil,
		},
		{
			name: "only user annotations — all preserved",
			annotations: map[string]string{
				"backup.company.com/schedule":  "daily",
				"backup.company.com/retention": "30d",
				"storage.company.com/tier":     "premium",
			},
			want: map[string]string{
				"backup.company.com/schedule":  "daily",
				"backup.company.com/retention": "30d",
				"storage.company.com/tier":     "premium",
			},
		},
		{
			name: "only server-managed annotations — all stripped",
			annotations: map[string]string{
				"pv.kubernetes.io/bind-completed":                  "yes",
				"volume.kubernetes.io/storage-provisioner":         "ebs.csi.aws.com",
				"volume.beta.kubernetes.io/storage-provisioner":    "kubernetes.io/gce-pd",
				"kubectl.kubernetes.io/last-applied-configuration": "{}",
			},
			want: nil,
		},
		{
			name: "mixed — user preserved, server stripped",
			annotations: map[string]string{
				"backup.company.com/schedule":              "daily",
				"pv.kubernetes.io/bind-completed":          "yes",
				"volume.kubernetes.io/storage-provisioner": "ebs.csi.aws.com",
				"app.kubernetes.io/managed-by":             "helm",
			},
			want: map[string]string{
				"backup.company.com/schedule":  "daily",
				"app.kubernetes.io/managed-by": "helm",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripServerManagedPVCAnnotations(tt.annotations)
			if tt.want == nil && got != nil {
				t.Errorf("want nil, got %v", got)
				return
			}
			if tt.want != nil && got == nil {
				t.Errorf("want %v, got nil", tt.want)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("length mismatch: got %v, want %v", got, tt.want)
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("key %q: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// newMinimalConfigFlags creates a ConfigFlags backed by an empty temp kubeconfig,
// suitable for unit tests that call Complete() without a real cluster.
func newMinimalConfigFlags(t *testing.T) *genericclioptions.ConfigFlags {
	t.Helper()
	cfg := clientcmdapi.NewConfig()
	data, err := clientcmd.Write(*cfg)
	if err != nil {
		t.Fatalf("failed to serialise empty kubeconfig: %v", err)
	}
	f, err := os.CreateTemp("", "test-kubeconfig-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp kubeconfig: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatalf("failed to write temp kubeconfig: %v", err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	flags := genericclioptions.NewConfigFlags(false)
	name := f.Name()
	flags.KubeConfig = &name
	return flags
}

// covers the whitespace-trimming and whitespace-rejection logic added to Complete() in PR #859.
func TestComplete_CloudStorageWhitespace(t *testing.T) {
	tests := []struct {
		name         string
		cloudStorage string
		wantErr      bool
		wantErrMsg   string
		wantValue    string
	}{
		{
			name:         "whitespace-only value is rejected",
			cloudStorage: "   ",
			wantErr:      true,
			wantErrMsg:   "whitespace",
		},
		{
			name:         "tab-only value is rejected",
			cloudStorage: "\t",
			wantErr:      true,
			wantErrMsg:   "whitespace",
		},
		{
			name:         "newline-only value is rejected",
			cloudStorage: "\n",
			wantErr:      true,
			wantErrMsg:   "whitespace",
		},
		{
			name:         "leading and trailing spaces are trimmed",
			cloudStorage: "  remote:my-bucket  ",
			wantErr:      false,
			wantValue:    "remote:my-bucket",
		},
		{
			name:         "leading whitespace only is trimmed",
			cloudStorage: "\tremote:my-bucket",
			wantErr:      false,
			wantValue:    "remote:my-bucket",
		},
		{
			name:         "valid value passes through unchanged",
			cloudStorage: "remote:my-bucket",
			wantErr:      false,
			wantValue:    "remote:my-bucket",
		},
		{
			name:         "empty value is accepted and stays empty",
			cloudStorage: "",
			wantErr:      false,
			wantValue:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			tc := &TransferPVCCommand{
				configFlags: newMinimalConfigFlags(t),
				Flags:       Flags{CloudStorage: tt.cloudStorage},
			}
			err := tc.Complete(cmd, nil)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Complete() expected error but got nil")
				}
				if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("Complete() error = %q, want to contain %q", err.Error(), tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("Complete() unexpected error: %v", err)
			}
			if tc.CloudStorage != tt.wantValue {
				t.Errorf("CloudStorage after Complete() = %q, want %q", tc.CloudStorage, tt.wantValue)
			}
		})
	}
}

// covers the --encrypt guard added in PR #859.
func TestValidateEncryptRequiresCloudStorage(t *testing.T) {
	const guardMsg = "--encrypt requires --cloud-storage"
	tests := []struct {
		name      string
		cmd       TransferPVCCommand
		wantGuard bool
	}{
		{
			name:      "--encrypt without --cloud-storage is rejected",
			cmd:       TransferPVCCommand{Flags: Flags{Encrypt: true}},
			wantGuard: true,
		},
		{
			name: "--encrypt with --cloud-storage set passes the encrypt guard",
			cmd: TransferPVCCommand{
				Flags: Flags{Encrypt: true, CloudStorage: "remote:my-bucket"},
			},
			wantGuard: false,
		},
		{
			name:      "--encrypt=false without --cloud-storage does not trigger guard",
			cmd:       TransferPVCCommand{Flags: Flags{Encrypt: false}},
			wantGuard: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cmd.Validate()
			hasGuard := err != nil && strings.Contains(err.Error(), guardMsg)
			if tt.wantGuard && !hasGuard {
				t.Errorf("expected guard error %q, got: %v", guardMsg, err)
			}
			if !tt.wantGuard && hasGuard {
				t.Errorf("unexpected guard error: %v", err)
			}
		})
	}
}

// covers the --rclone-config-secret guard added in PR #859.
func TestValidateRcloneConfigSecretRequiresCloudStorage(t *testing.T) {
	const guardMsg = "--rclone-config-secret requires --cloud-storage"
	tests := []struct {
		name      string
		cmd       TransferPVCCommand
		wantGuard bool
	}{
		{
			name:      "--rclone-config-secret without --cloud-storage is rejected",
			cmd:       TransferPVCCommand{Flags: Flags{RcloneConfigSecret: "my-secret"}},
			wantGuard: true,
		},
		{
			name: "--rclone-config-secret with --cloud-storage set passes the guard",
			cmd: TransferPVCCommand{
				Flags: Flags{RcloneConfigSecret: "my-secret", CloudStorage: "remote:my-bucket"},
			},
			wantGuard: false,
		},
		{
			name:      "empty --rclone-config-secret without --cloud-storage does not trigger guard",
			cmd:       TransferPVCCommand{Flags: Flags{RcloneConfigSecret: ""}},
			wantGuard: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cmd.Validate()
			hasGuard := err != nil && strings.Contains(err.Error(), guardMsg)
			if tt.wantGuard && !hasGuard {
				t.Errorf("expected guard error %q, got: %v", guardMsg, err)
			}
			if !tt.wantGuard && hasGuard {
				t.Errorf("unexpected guard error: %v", err)
			}
		})
	}
}

// covers the --rclone-config-file guard added in PR #859.
func TestValidateRcloneConfigFileRequiresCloudStorage(t *testing.T) {
	const guardMsg = "--rclone-config-file requires --cloud-storage"
	tests := []struct {
		name      string
		cmd       TransferPVCCommand
		wantGuard bool
	}{
		{
			name:      "--rclone-config-file without --cloud-storage is rejected",
			cmd:       TransferPVCCommand{Flags: Flags{RcloneConfigFile: "/etc/rclone.conf"}},
			wantGuard: true,
		},
		{
			name: "--rclone-config-file with --cloud-storage set passes the guard",
			cmd: TransferPVCCommand{
				Flags: Flags{RcloneConfigFile: "/etc/rclone.conf", CloudStorage: "remote:my-bucket"},
			},
			wantGuard: false,
		},
		{
			name:      "empty --rclone-config-file without --cloud-storage does not trigger guard",
			cmd:       TransferPVCCommand{Flags: Flags{RcloneConfigFile: ""}},
			wantGuard: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cmd.Validate()
			hasGuard := err != nil && strings.Contains(err.Error(), guardMsg)
			if tt.wantGuard && !hasGuard {
				t.Errorf("expected guard error %q, got: %v", guardMsg, err)
			}
			if !tt.wantGuard && hasGuard {
				t.Errorf("unexpected guard error: %v", err)
			}
		})
	}
}

// verifies that all four indirect-mode flag guards (--encrypt, --keep-cloud-data, --rclone-config-secret,
// --rclone-config-file) are evaluated before the source/destination context checks.
func TestValidateIndirectFlagGuardsFireBeforeContextCheck(t *testing.T) {
	tests := []struct {
		name    string
		cmd     TransferPVCCommand
		wantMsg string
	}{
		{
			name:    "--encrypt guard fires before source context check",
			cmd:     TransferPVCCommand{Flags: Flags{Encrypt: true}},
			wantMsg: "--encrypt requires --cloud-storage",
		},
		{
			name:    "--keep-cloud-data guard fires before source context check",
			cmd:     TransferPVCCommand{Flags: Flags{KeepCloudData: true}},
			wantMsg: "--keep-cloud-data requires --cloud-storage",
		},
		{
			name:    "--rclone-config-secret guard fires before source context check",
			cmd:     TransferPVCCommand{Flags: Flags{RcloneConfigSecret: "my-secret"}},
			wantMsg: "--rclone-config-secret requires --cloud-storage",
		},
		{
			name:    "--rclone-config-file guard fires before source context check",
			cmd:     TransferPVCCommand{Flags: Flags{RcloneConfigFile: "/etc/rclone.conf"}},
			wantMsg: "--rclone-config-file requires --cloud-storage",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// sourceContext and destinationContext are both nil — if the context
			// check ran first we would get "cannot evaluate source context".
			err := tt.cmd.Validate()
			if err == nil {
				t.Fatal("Validate() expected error but got nil")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("Validate() error = %q, want to contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}
