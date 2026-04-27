package transfer_pvc

import (
	"testing"

	rsynctransfer "github.com/backube/pvc-transfer/transfer/rsync"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test_parseSourceDestinationMapping verifies parsing of "source:destination" mapping strings.
// This format is used in CLI flags to specify source and destination names for PVCs/namespaces.
// Supports both "name" (same for both) and "source:destination" formats.
func Test_parseSourceDestinationMapping(t *testing.T) {
	tests := []struct {
		name            string
		mapping         string
		wantSource      string
		wantDestination string
		wantErr         bool
	}{
		// Single name sets both source and destination to the same value
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

// TestBuildDestinationPVC_BasicCopy verifies that buildDestinationPVC correctly
// copies essential PVC properties (labels, access modes, storage requests) from
// the source PVC while updating the name and namespace to destination values.
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
}

// TestBuildDestinationPVC_StorageRequestsOverride verifies that the --storage-requests flag
// overrides the source PVC's storage size when creating the destination PVC.
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

// TestBuildDestinationPVC_FieldClearing verifies that VolumeMode and VolumeName are cleared.
// These must be nil/empty so the destination PVC can bind to a new PV rather than
// attempting to reference the source cluster's PV.
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
	// Verify other flags are preserved
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
