package framework

import (
	"testing"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHasDefaultStorageClassAnnotation(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantDefault bool
	}{
		{
			name: "ga annotation marks default",
			annotations: map[string]string{
				defaultStorageClassAnnotation: "true",
			},
			wantDefault: true,
		},
		{
			name: "beta annotation marks default",
			annotations: map[string]string{
				defaultStorageClassBetaAnnotation: "true",
			},
			wantDefault: true,
		},
		{
			name: "false annotation is not default",
			annotations: map[string]string{
				defaultStorageClassAnnotation: "false",
			},
			wantDefault: false,
		},
		{
			name:        "missing annotations is not default",
			annotations: nil,
			wantDefault: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storageClass := storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
			}

			if got := hasDefaultStorageClassAnnotation(storageClass); got != tt.wantDefault {
				t.Fatalf("hasDefaultStorageClassAnnotation() = %v, want %v", got, tt.wantDefault)
			}
		})
	}
}

func TestAssertTransferPVCProgressOutput(t *testing.T) {
	const output = `
[1/3] Reading source PVC ... ok
[2/3] Creating endpoint (nginx-ingress) ... ok
[3/3] Copying data (rsync) ... finished  exit=0

Summary
-------
PVC data copy: succeeded
duration:      1s
Done.
`

	tests := []struct {
		name    string
		output  string
		phases  []string
		summary string
		wantErr bool
	}{
		{
			name:    "accepts complete direct output",
			output:  output,
			phases:  []string{"Reading source PVC", "Creating endpoint", "Copying data (rsync)"},
			summary: "PVC data copy: succeeded",
		},
		{
			name:    "reports missing phase",
			output:  output,
			phases:  []string{"Reading source PVC", "Cleaning up temporary resources"},
			summary: "PVC data copy: succeeded",
			wantErr: true,
		},
		{
			name:    "reports missing summary status",
			output:  output,
			phases:  []string{"Reading source PVC"},
			summary: "PVC data copy: failed",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AssertTransferPVCProgressOutput(tt.output, 3, tt.phases, tt.summary)
			if (err != nil) != tt.wantErr {
				t.Fatalf("AssertTransferPVCProgressOutput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
