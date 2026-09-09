package transfer_pvc

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane-lib/transform/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestBuildDestinationPVCMatchesKubernetesPluginFilesystemCleanup(t *testing.T) {
	storageClass := "standard"
	filesystem := corev1.PersistentVolumeFilesystem
	source := &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "source-pvc",
			Namespace:       "source-ns",
			Labels:          map[string]string{"app": "example"},
			Annotations:     map[string]string{"keep": "value", "pv.kubernetes.io/bind-completed": "yes", "kubectl.kubernetes.io/last-applied-configuration": "{}"},
			Finalizers:      []string{"kubernetes.io/pvc-protection"},
			ResourceVersion: "123",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: &storageClass,
			VolumeMode:       &filesystem,
			VolumeName:       "pvc-source-id",
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Gi"),
			}},
		},
	}

	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(source)
	if err != nil {
		t.Fatalf("convert source PVC to unstructured: %v", err)
	}
	response, err := (&kubernetes.KubernetesTransformPlugin{}).Run(transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: object},
	})
	if err != nil {
		t.Fatalf("run KubernetesPlugin: %v", err)
	}
	if response.IsWhiteOut {
		t.Fatal("KubernetesPlugin unexpectedly whiteouted PVC")
	}

	sourceJSON, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("marshal source PVC: %v", err)
	}
	cleanedJSON, err := response.Patches.Apply(sourceJSON)
	if err != nil {
		t.Fatalf("apply KubernetesPlugin patches: %v", err)
	}
	var cleaned corev1.PersistentVolumeClaim
	if err := json.Unmarshal(cleanedJSON, &cleaned); err != nil {
		t.Fatalf("unmarshal cleaned PVC: %v", err)
	}

	transferred := (&TransferPVCCommand{Flags: Flags{PVC: PvcFlags{
		Name:      mappedNameVar{destination: "target-pvc"},
		Namespace: mappedNameVar{destination: "target-ns"},
	}}}).buildDestinationPVC(source)

	if cleaned.Spec.VolumeMode == nil || *cleaned.Spec.VolumeMode != corev1.PersistentVolumeFilesystem {
		t.Fatalf("KubernetesPlugin volumeMode = %v, want Filesystem", cleaned.Spec.VolumeMode)
	}
	if transferred.Spec.VolumeMode != nil {
		t.Fatalf("transfer-pvc volumeMode = %v, want nil to use Kubernetes Filesystem default", *transferred.Spec.VolumeMode)
	}
	cleaned.Spec.VolumeMode = nil

	if diff := cmp.Diff(cleaned.Labels, transferred.Labels); diff != "" {
		t.Errorf("labels differ (-KubernetesPlugin +transfer-pvc):\n%s", diff)
	}
	if diff := cmp.Diff(cleaned.Annotations, transferred.Annotations); diff != "" {
		t.Errorf("annotations differ (-KubernetesPlugin +transfer-pvc):\n%s", diff)
	}
	if diff := cmp.Diff(cleaned.Spec, transferred.Spec); diff != "" {
		t.Errorf("PVC spec differs after cleanup (-KubernetesPlugin +transfer-pvc):\n%s", diff)
	}
	if len(cleaned.Finalizers) != 0 || len(transferred.Finalizers) != 0 {
		t.Errorf("finalizers must be removed: KubernetesPlugin=%v transfer-pvc=%v", cleaned.Finalizers, transferred.Finalizers)
	}
}
