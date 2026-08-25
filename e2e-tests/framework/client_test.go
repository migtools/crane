package framework

import (
	"testing"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHasDefaultStorageClassAnnotation(t *testing.T) {
	tests := []struct {
		name         string
		annotations  map[string]string
		wantDefault  bool
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
