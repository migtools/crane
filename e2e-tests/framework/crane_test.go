package framework

import (
	"reflect"
	"testing"
)

func TestBuildTransferPVCArgs(t *testing.T) {
	tests := []struct {
		name string
		opts TransferPVCOptions
		want []string
	}{
		{
			name: "default direct transfer omits custom image",
			opts: TransferPVCOptions{
				SourceContext:   "src",
				TargetContext:   "tgt",
				PVCName:         "data",
				PVCNamespaceMap: "source:target",
				Endpoint:        "route",
			},
			want: []string{"transfer-pvc", "--source-context", "src", "--destination-context", "tgt", "--pvc-name", "data", "--pvc-namespace", "source:target", "--endpoint", "route"},
		},
		{
			name: "custom image applies to direct transfer",
			opts: TransferPVCOptions{
				SourceContext:   "src",
				TargetContext:   "tgt",
				PVCName:         "data",
				PVCNamespaceMap: "source:target",
				RsyncImage:      "registry.example/rsync:test",
				Endpoint:        "route",
			},
			want: []string{"transfer-pvc", "--source-context", "src", "--destination-context", "tgt", "--pvc-name", "data", "--pvc-namespace", "source:target", "--source-image", "registry.example/rsync:test", "--destination-image", "registry.example/rsync:test", "--endpoint", "route"},
		},
		{
			name: "custom image applies to indirect transfer",
			opts: TransferPVCOptions{
				SourceContext:      "src",
				TargetContext:      "tgt",
				PVCName:            "data",
				PVCNamespaceMap:    "source:target",
				RsyncImage:         "registry.example/rsync:test",
				CloudStorage:       "remote:bucket",
				RcloneConfigSecret: "rclone-config",
			},
			want: []string{"transfer-pvc", "--source-context", "src", "--destination-context", "tgt", "--pvc-name", "data", "--pvc-namespace", "source:target", "--source-image", "registry.example/rsync:test", "--destination-image", "registry.example/rsync:test", "--cloud-storage", "remote:bucket", "--rclone-config-secret", "rclone-config"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildTransferPVCArgs(tt.opts); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("buildTransferPVCArgs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
