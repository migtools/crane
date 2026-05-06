package transfer_pvc

import (
	"context"
	"strings"
	"testing"

	rsynctransfer "github.com/backube/pvc-transfer/transfer/rsync"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	securityv1 "github.com/openshift/api/security/v1"
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

// Tests TransferPVCCommand.Validate for various error conditions and success scenarios
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

// Tests EndpointFlags.Validate for endpoint type validation
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

// Tests PvcFlags.Validate for PVC name and namespace validation
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

func TestMappedNameVar_Set(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantSource      string
		wantDestination string
		wantErr         bool
	}{
		{
			name:            "parses source:destination format",
			input:           "src-pvc:dest-pvc",
			wantSource:      "src-pvc",
			wantDestination: "dest-pvc",
			wantErr:         false,
		},
		{
			name:            "single value sets both source and destination",
			input:           "my-pvc",
			wantSource:      "my-pvc",
			wantDestination: "my-pvc",
			wantErr:         false,
		},
		{
			name:    "empty string returns error",
			input:   "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mappedNameVar{}
			err := m.Set(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("mappedNameVar.Set() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if m.source != tt.wantSource {
					t.Errorf("mappedNameVar.Set() source = %v, want %v", m.source, tt.wantSource)
				}
				if m.destination != tt.wantDestination {
					t.Errorf("mappedNameVar.Set() destination = %v, want %v", m.destination, tt.wantDestination)
				}
			}
		})
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

// Tests buildDestinationPVC to ensure source PVC fields are correctly copied to destination
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

// Tests buildDestinationPVC with storage requests override from flags
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

// Tests buildDestinationPVC with storage class override from flags
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

// Tests buildDestinationPVC clears VolumeMode and VolumeName from destination
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

// Tests verify.ApplyTo adds --checksum flag when verify is true
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

// Tests verify.ApplyTo removes checksum flags when verify is false
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

// Tests restrictedContainers.ApplyTo disables privilege options when true
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

// Tests restrictedContainers.ApplyTo enables privilege options when false
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

// Tests verbose.ApplyTo sets Info array and appends --progress flag
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

// Tests getRouteHostName returns nil when route name prefix is within 62 char limit
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

// Tests getRouteHostName returns truncated hostname when route name prefix exceeds 62 char limit
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

// Tests getRouteHostName returns error when Ingress config is not found
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

// Tests getNodeNameForPVC returns node name when a running pod with matching PVC volume is found
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

// Tests getNodeNameForPVC returns empty string when no matching pods are found
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

// Tests getNodeNameForPVC skips pods not in Running phase
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

// Tests getIDsForNamespace extracts UID from namespace annotations
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

// Tests getIDsForNamespace extracts GID from SupplementalGroups annotation
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

// Tests getIDsForNamespace returns SecurityContext with nils when annotations are missing
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

// Tests getIDsForNamespace returns error when namespace does not exist
func TestGetIDsForNamespace_ErrorOnMissingNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	_, err := getIDsForNamespace(fakeClient, "nonexistent-ns")
	if err == nil {
		t.Error("getIDsForNamespace() expected error for missing namespace, got nil")
	}
}

// Tests deleteResourcesIteratively successfully deletes all resources matching labels
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

// Tests deleteResourcesIteratively deletes multiple resource types in one call
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

// Tests deleteResourcesIteratively only deletes resources with matching labels
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

// Tests deleteResourcesIteratively only deletes resources in the specified namespace
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

// Tests deleteResourcesIteratively returns no error when no resources exist
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

// Tests garbageCollect deletes Pods, ConfigMaps, Secrets from source cluster
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
	if !strings.HasPrefix(got, "crane-") {
		t.Errorf("getValidatedResourceName() = %v, expected to start with 'crane-'", got)
	}
}
