package transfer_pvc

import (
	"context"
	"fmt"
	"strings"
	"testing"

	rsynctransfer "github.com/backube/pvc-transfer/transfer/rsync"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func int64Ptr(v int64) *int64 { return &v }

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
	got, err := getIDsForNamespace(c, "ocp-ns", "any-pvc")
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
	got, err := getIDsForNamespace(c, "k8s-ns", "mysql-data")
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
	got, err := getIDsForNamespace(c, "ocp-ns", "mysql-data")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RunAsUser == nil || *got.RunAsUser != 1000700000 {
		t.Errorf("OCP annotation should take precedence: RunAsUser = %v, want 1000700000", fmtInt64Ptr(got.RunAsUser))
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

func TestTransferPVCCommand_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cmd     TransferPVCCommand
		wantErr bool
		errMsg  string
	}{
		{
			name: "source context nil returns error",
			cmd: TransferPVCCommand{
				sourceContext:      nil,
				destinationContext: &clientcmdapi.Context{Cluster: "dest-cluster"},
			},
			wantErr: true,
			errMsg:  "cannot evaluate source context",
		},
		{
			name: "destination context nil returns error",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "source-cluster"},
				destinationContext: nil,
			},
			wantErr: true,
			errMsg:  "cannot evaluate destination context",
		},
		{
			name: "same cluster for source and destination returns error",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "same-cluster"},
				destinationContext: &clientcmdapi.Context{Cluster: "same-cluster"},
			},
			wantErr: true,
			errMsg:  "both source and destination cluster are the same",
		},
		{
			name: "PVC validation failure cascades error",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "source-cluster"},
				destinationContext: &clientcmdapi.Context{Cluster: "dest-cluster"},
				Flags: Flags{
					PVC: PvcFlags{
						Name:      mappedNameVar{source: "", destination: "dest-pvc"},
						Namespace: mappedNameVar{source: "src-ns", destination: "dest-ns"},
					},
				},
			},
			wantErr: true,
			errMsg:  "source pvc name cannot be empty",
		},
		{
			name: "endpoint validation failure cascades error",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "source-cluster"},
				destinationContext: &clientcmdapi.Context{Cluster: "dest-cluster"},
				Flags: Flags{
					PVC: PvcFlags{
						Name:      mappedNameVar{source: "src-pvc", destination: "dest-pvc"},
						Namespace: mappedNameVar{source: "src-ns", destination: "dest-ns"},
					},
					Endpoint: EndpointFlags{
						Type:      endpointNginx,
						Subdomain: "",
					},
				},
			},
			wantErr: true,
			errMsg:  "subdomain cannot be empty when using nginx ingress",
		},
		{
			name: "validation succeeds with all required fields",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "source-cluster"},
				destinationContext: &clientcmdapi.Context{Cluster: "dest-cluster"},
				Flags: Flags{
					PVC: PvcFlags{
						Name:      mappedNameVar{source: "src-pvc", destination: "dest-pvc"},
						Namespace: mappedNameVar{source: "src-ns", destination: "dest-ns"},
					},
					Endpoint: EndpointFlags{
						Type:      endpointNginx,
						Subdomain: "my.subdomain.example.com",
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cmd.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("TransferPVCCommand.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("TransferPVCCommand.Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestEndpointFlags_Validate(t *testing.T) {
	tests := []struct {
		name    string
		flags   EndpointFlags
		wantErr bool
		errMsg  string
	}{
		{
			name: "empty type defaults to nginx and requires subdomain",
			flags: EndpointFlags{
				Type:      "",
				Subdomain: "",
			},
			wantErr: true,
			errMsg:  "subdomain cannot be empty when using nginx ingress",
		},
		{
			name: "nginx type missing subdomain returns error",
			flags: EndpointFlags{
				Type:      endpointNginx,
				Subdomain: "",
			},
			wantErr: true,
			errMsg:  "subdomain cannot be empty when using nginx ingress",
		},
		{
			name: "nginx type with subdomain passes validation",
			flags: EndpointFlags{
				Type:      endpointNginx,
				Subdomain: "my.subdomain.example.com",
			},
			wantErr: false,
		},
		{
			name: "route type does not require subdomain",
			flags: EndpointFlags{
				Type:      endpointRoute,
				Subdomain: "",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.flags.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("EndpointFlags.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("EndpointFlags.Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestPvcFlags_Validate(t *testing.T) {
	tests := []struct {
		name    string
		flags   PvcFlags
		wantErr bool
		errMsg  string
	}{
		{
			name: "empty source name returns error",
			flags: PvcFlags{
				Name:      mappedNameVar{source: "", destination: "dest-pvc"},
				Namespace: mappedNameVar{source: "src-ns", destination: "dest-ns"},
			},
			wantErr: true,
			errMsg:  "source pvc name cannot be empty",
		},
		{
			name: "empty destination name returns error",
			flags: PvcFlags{
				Name:      mappedNameVar{source: "src-pvc", destination: ""},
				Namespace: mappedNameVar{source: "src-ns", destination: "dest-ns"},
			},
			wantErr: true,
			errMsg:  "destnation pvc name cannot be empty",
		},
		{
			name: "empty source namespace returns error",
			flags: PvcFlags{
				Name:      mappedNameVar{source: "src-pvc", destination: "dest-pvc"},
				Namespace: mappedNameVar{source: "", destination: "dest-ns"},
			},
			wantErr: true,
			errMsg:  "source pvc namespace cannot be empty",
		},
		{
			name: "empty destination namespace returns error",
			flags: PvcFlags{
				Name:      mappedNameVar{source: "src-pvc", destination: "dest-pvc"},
				Namespace: mappedNameVar{source: "src-ns", destination: ""},
			},
			wantErr: true,
			errMsg:  "destination pvc namespace cannot be empty",
		},
		{
			name: "all fields populated passes validation",
			flags: PvcFlags{
				Name:      mappedNameVar{source: "src-pvc", destination: "dest-pvc"},
				Namespace: mappedNameVar{source: "src-ns", destination: "dest-ns"},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.flags.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("PvcFlags.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("PvcFlags.Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestMappedNameVar_String(t *testing.T) {
	m := &mappedNameVar{source: "my-pvc", destination: "new-pvc"}
	got := m.String()
	want := "my-pvc:new-pvc"
	if got != want {
		t.Errorf("mappedNameVar.String() = %v, want %v", got, want)
	}
}

func TestMappedNameVar_Type(t *testing.T) {
	m := &mappedNameVar{}
	got := m.Type()
	want := "string"
	if got != want {
		t.Errorf("mappedNameVar.Type() = %v, want %v", got, want)
	}
}

func TestQuantityVar_String(t *testing.T) {
	q := &quantityVar{}
	_ = q.Set("10Gi")
	got := q.String()
	if got != "10Gi" {
		t.Errorf("quantityVar.String() = %v, want %v", got, "10Gi")
	}
}

func TestQuantityVar_Set(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid quantity parses successfully",
			input:   "5Gi",
			wantErr: false,
		},
		{
			name:    "invalid quantity returns error",
			input:   "invalid",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &quantityVar{}
			err := q.Set(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("quantityVar.Set() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && q.quantity == nil {
				t.Errorf("quantityVar.Set() quantity should not be nil after successful set")
			}
		})
	}
}

func TestQuantityVar_Type(t *testing.T) {
	q := &quantityVar{}
	got := q.Type()
	want := "string"
	if got != want {
		t.Errorf("quantityVar.Type() = %v, want %v", got, want)
	}
}

func TestEndpointType_Set(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    endpointType
		wantErr bool
	}{
		{
			name:    "nginx-ingress is valid",
			input:   "nginx-ingress",
			want:    endpointNginx,
			wantErr: false,
		},
		{
			name:    "route is valid",
			input:   "route",
			want:    endpointRoute,
			wantErr: false,
		},
		{
			name:    "invalid type returns error",
			input:   "invalid-type",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var e endpointType
			err := e.Set(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("endpointType.Set() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && e != tt.want {
				t.Errorf("endpointType.Set() = %v, want %v", e, tt.want)
			}
		})
	}
}

func TestEndpointType_Type(t *testing.T) {
	e := endpointNginx
	got := e.Type()
	want := "string"
	if got != want {
		t.Errorf("endpointType.Type() = %v, want %v", got, want)
	}
}

func TestBuildDestinationPVC_BasicCopy(t *testing.T) {
	sourcePVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "source-pvc",
			Namespace: "source-ns",
			Labels: map[string]string{
				"app": "myapp",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	cmd := &TransferPVCCommand{
		Flags: Flags{
			PVC: PvcFlags{
				Name:      mappedNameVar{source: "source-pvc", destination: "dest-pvc"},
				Namespace: mappedNameVar{source: "source-ns", destination: "dest-ns"},
			},
		},
	}

	destPVC := cmd.buildDestinationPVC(sourcePVC)

	if destPVC.Name != "dest-pvc" {
		t.Errorf("buildDestinationPVC() Name = %v, want %v", destPVC.Name, "dest-pvc")
	}
	if destPVC.Namespace != "dest-ns" {
		t.Errorf("buildDestinationPVC() Namespace = %v, want %v", destPVC.Namespace, "dest-ns")
	}
	if destPVC.Labels["app"] != "myapp" {
		t.Errorf("buildDestinationPVC() Labels[app] = %v, want %v", destPVC.Labels["app"], "myapp")
	}
	if len(destPVC.Spec.AccessModes) != 1 || destPVC.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Errorf("buildDestinationPVC() AccessModes not copied correctly")
	}
	if !destPVC.Spec.Resources.Requests[corev1.ResourceStorage].Equal(sourcePVC.Spec.Resources.Requests[corev1.ResourceStorage]) {
		t.Errorf("buildDestinationPVC() storage request = %v, want %v", destPVC.Spec.Resources.Requests[corev1.ResourceStorage], sourcePVC.Spec.Resources.Requests[corev1.ResourceStorage])
	}
}

func TestBuildDestinationPVC_StorageRequestsOverride(t *testing.T) {
	sourcePVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "source-pvc",
			Namespace: "source-ns",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	overrideQuantity := resource.MustParse("50Gi")
	cmd := &TransferPVCCommand{
		Flags: Flags{
			PVC: PvcFlags{
				Name:            mappedNameVar{source: "source-pvc", destination: "dest-pvc"},
				Namespace:       mappedNameVar{source: "source-ns", destination: "dest-ns"},
				StorageRequests: quantityVar{quantity: &overrideQuantity},
			},
		},
	}

	destPVC := cmd.buildDestinationPVC(sourcePVC)

	got := destPVC.Spec.Resources.Requests[corev1.ResourceStorage]
	if !got.Equal(overrideQuantity) {
		t.Errorf("buildDestinationPVC() storage request = %v, want %v", got.String(), overrideQuantity.String())
	}
}

func TestBuildDestinationPVC_StorageClassOverride(t *testing.T) {
	sourceStorageClass := "source-storage-class"
	sourcePVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "source-pvc",
			Namespace: "source-ns",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &sourceStorageClass,
		},
	}

	cmd := &TransferPVCCommand{
		Flags: Flags{
			PVC: PvcFlags{
				Name:             mappedNameVar{source: "source-pvc", destination: "dest-pvc"},
				Namespace:        mappedNameVar{source: "source-ns", destination: "dest-ns"},
				StorageClassName: "dest-storage-class",
			},
		},
	}

	destPVC := cmd.buildDestinationPVC(sourcePVC)

	if destPVC.Spec.StorageClassName == nil {
		t.Fatalf("buildDestinationPVC() StorageClassName is nil, want dest-storage-class")
	}
	if *destPVC.Spec.StorageClassName != "dest-storage-class" {
		t.Errorf("buildDestinationPVC() StorageClassName = %v, want %v", *destPVC.Spec.StorageClassName, "dest-storage-class")
	}
}

func TestBuildDestinationPVC_FieldClearing(t *testing.T) {
	volumeMode := corev1.PersistentVolumeFilesystem
	sourcePVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "source-pvc",
			Namespace: "source-ns",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeMode: &volumeMode,
			VolumeName: "source-pv",
		},
	}

	cmd := &TransferPVCCommand{
		Flags: Flags{
			PVC: PvcFlags{
				Name:      mappedNameVar{source: "source-pvc", destination: "dest-pvc"},
				Namespace: mappedNameVar{source: "source-ns", destination: "dest-ns"},
			},
		},
	}

	destPVC := cmd.buildDestinationPVC(sourcePVC)

	if destPVC.Spec.VolumeMode != nil {
		t.Errorf("buildDestinationPVC() VolumeMode = %v, want nil", destPVC.Spec.VolumeMode)
	}
	if destPVC.Spec.VolumeName != "" {
		t.Errorf("buildDestinationPVC() VolumeName = %v, want empty string", destPVC.Spec.VolumeName)
	}
}

func TestVerifyApplyTo_EnableChecksum(t *testing.T) {
	opts := &rsynctransfer.CommandOptions{
		Extras: []string{"--existing-flag"},
	}

	v := verify(true)
	err := v.ApplyTo(opts)
	if err != nil {
		t.Fatalf("verify.ApplyTo() returned error: %v", err)
	}

	found := false
	for _, extra := range opts.Extras {
		if extra == "--checksum" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("verify(true).ApplyTo() did not add --checksum flag, Extras = %v", opts.Extras)
	}
}

func TestVerifyApplyTo_DisableChecksum(t *testing.T) {
	opts := &rsynctransfer.CommandOptions{
		Extras: []string{"--checksum", "-c", "--other-flag"},
	}

	v := verify(false)
	err := v.ApplyTo(opts)
	if err != nil {
		t.Fatalf("verify.ApplyTo() returned error: %v", err)
	}

	for _, extra := range opts.Extras {
		if extra == "--checksum" || extra == "-c" {
			t.Errorf("verify(false).ApplyTo() did not remove %s flag, Extras = %v", extra, opts.Extras)
		}
	}
	found := false
	for _, extra := range opts.Extras {
		if extra == "--other-flag" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("verify(false).ApplyTo() removed non-checksum flag, Extras = %v", opts.Extras)
	}
}

func TestRestrictedContainers_TrueDisablesPrivileges(t *testing.T) {
	opts := &rsynctransfer.CommandOptions{
		Groups:       true,
		Owners:       true,
		DeviceFiles:  true,
		SpecialFiles: true,
		Extras:       []string{},
	}

	r := restrictedContainers(true)
	err := r.ApplyTo(opts)
	if err != nil {
		t.Fatalf("restrictedContainers.ApplyTo() returned error: %v", err)
	}

	if opts.Groups {
		t.Error("restrictedContainers(true).ApplyTo() did not set Groups to false")
	}
	if opts.Owners {
		t.Error("restrictedContainers(true).ApplyTo() did not set Owners to false")
	}
	if opts.DeviceFiles {
		t.Error("restrictedContainers(true).ApplyTo() did not set DeviceFiles to false")
	}
	if opts.SpecialFiles {
		t.Error("restrictedContainers(true).ApplyTo() did not set SpecialFiles to false")
	}

	found := false
	for _, extra := range opts.Extras {
		if extra == "--omit-dir-times" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("restrictedContainers(true).ApplyTo() did not add --omit-dir-times, Extras = %v", opts.Extras)
	}
}

func TestRestrictedContainers_FalseEnablesPrivileges(t *testing.T) {
	opts := &rsynctransfer.CommandOptions{
		Groups:       false,
		Owners:       false,
		DeviceFiles:  false,
		SpecialFiles: false,
		Extras:       []string{},
	}

	r := restrictedContainers(false)
	err := r.ApplyTo(opts)
	if err != nil {
		t.Fatalf("restrictedContainers.ApplyTo() returned error: %v", err)
	}

	if !opts.Groups {
		t.Error("restrictedContainers(false).ApplyTo() did not set Groups to true")
	}
	if !opts.Owners {
		t.Error("restrictedContainers(false).ApplyTo() did not set Owners to true")
	}
	if !opts.DeviceFiles {
		t.Error("restrictedContainers(false).ApplyTo() did not set DeviceFiles to true")
	}
	if !opts.SpecialFiles {
		t.Error("restrictedContainers(false).ApplyTo() did not set SpecialFiles to true")
	}
}

func TestVerboseApplyTo_SetsInfoAndProgress(t *testing.T) {
	opts := &rsynctransfer.CommandOptions{
		Info:   []string{},
		Extras: []string{},
	}

	v := verbose(true)
	err := v.ApplyTo(opts)
	if err != nil {
		t.Fatalf("verbose.ApplyTo() returned error: %v", err)
	}

	expectedInfo := []string{"COPY", "DEL", "STATS2", "PROGRESS2", "FLIST2"}
	if len(opts.Info) != len(expectedInfo) {
		t.Errorf("verbose.ApplyTo() Info length = %d, want %d", len(opts.Info), len(expectedInfo))
	}
	for i, v := range expectedInfo {
		if i < len(opts.Info) && opts.Info[i] != v {
			t.Errorf("verbose.ApplyTo() Info[%d] = %v, want %v", i, opts.Info[i], v)
		}
	}

	found := false
	for _, extra := range opts.Extras {
		if extra == "--progress" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("verbose.ApplyTo() did not add --progress flag, Extras = %v", opts.Extras)
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

	longName := "this-is-a-very-long-pvc-name-that-exceeds-the-limit"
	longNamespace := "this-is-also-a-long-namespace"
	namespacedName := types.NamespacedName{
		Name:      longName,
		Namespace: longNamespace,
	}

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

	truncated := truncateWithHash(prefix)
	expectedHostname := truncated + ".apps.example.com"
	if *hostname != expectedHostname {
		t.Errorf("getRouteHostName() = %q, want %q", *hostname, expectedHostname)
	}
}

func TestGetRouteHostName_LongPrefix_IngressNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = configv1.AddToScheme(scheme)

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

	svcList := &corev1.ServiceList{}
	if err := fakeClient.List(context.TODO(), svcList, client.InNamespace("test-ns"), client.MatchingLabels(labels)); err != nil {
		t.Fatalf("Failed to list services: %v", err)
	}
	if len(svcList.Items) != 0 {
		t.Errorf("deleteResourcesIteratively() did not delete service, still have %d items", len(svcList.Items))
	}

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

	targetList := &corev1.ServiceList{}
	if err := fakeClient.List(context.TODO(), targetList, client.InNamespace("target-ns")); err != nil {
		t.Fatalf("Failed to list services in target-ns: %v", err)
	}
	if len(targetList.Items) != 0 {
		t.Errorf("Service in target-ns should be deleted, still have %d items", len(targetList.Items))
	}

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

	ingressList := &networkingv1.IngressList{}
	_ = destClient.List(context.TODO(), ingressList, client.InNamespace("dest-ns"), client.MatchingLabels(labels))
	if len(ingressList.Items) != 0 {
		t.Errorf("garbageCollect() did not delete destination ingress, still have %d", len(ingressList.Items))
	}

	podList := &corev1.PodList{}
	_ = destClient.List(context.TODO(), podList, client.InNamespace("dest-ns"), client.MatchingLabels(labels))
	if len(podList.Items) != 0 {
		t.Errorf("garbageCollect() did not delete destination pods, still have %d", len(podList.Items))
	}
}

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

	routeList := &routev1.RouteList{}
	_ = destClient.List(context.TODO(), routeList, client.InNamespace("dest-ns"), client.MatchingLabels(labels))
	if len(routeList.Items) != 0 {
		t.Errorf("garbageCollect() did not delete destination route, still have %d", len(routeList.Items))
	}

	podList := &corev1.PodList{}
	_ = destClient.List(context.TODO(), podList, client.InNamespace("dest-ns"), client.MatchingLabels(labels))
	if len(podList.Items) != 0 {
		t.Errorf("garbageCollect() did not delete destination pods, still have %d", len(podList.Items))
	}
}

func TestGetValidatedResourceName_ShortName(t *testing.T) {
	shortName := "my-pvc"
	got := getValidatedResourceName(shortName)
	if got != shortName {
		t.Errorf("getValidatedResourceName() = %v, want %v", got, shortName)
	}
}

func TestGetValidatedResourceName_LongName(t *testing.T) {
	longName := "this-is-a-very-long-persistent-volume-claim-name-that-exceeds-63-characters-limit"
	got := getValidatedResourceName(longName)

	if len(got) >= 63 {
		t.Errorf("getValidatedResourceName() returned name of length %d, want < 63", len(got))
	}
	if !strings.HasPrefix(got, "crane-") {
		t.Errorf("getValidatedResourceName() = %v, expected to start with 'crane-'", got)
	}
}
