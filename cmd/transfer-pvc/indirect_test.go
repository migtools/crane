package transfer_pvc

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRcloneObscure(t *testing.T) {
	original := "test-encryption-password-42"
	obscured, err := rcloneObscure(original, logrus.StandardLogger())
	if err != nil {
		t.Fatalf("rcloneObscure() error: %v", err)
	}
	if obscured == "" {
		t.Fatal("rcloneObscure() returned empty string")
	}
	if obscured == original {
		t.Error("obscured value should differ from original")
	}

	// Round-trip: reveal the obscured value using the same algorithm
	revealed, err := rcloneReveal(obscured)
	if err != nil {
		t.Fatalf("rcloneReveal() error: %v", err)
	}
	if revealed != original {
		t.Errorf("round-trip failed: got %q, want %q", revealed, original)
	}
}

func TestRcloneObscure_DifferentOutputEachCall(t *testing.T) {
	a, _ := rcloneObscure("same-input", logrus.StandardLogger())
	b, _ := rcloneObscure("same-input", logrus.StandardLogger())
	if a == b {
		t.Error("two calls with same input should produce different output (random IV)")
	}
}

func TestGenerateCryptSection(t *testing.T) {
	section, err := generateCryptSection("remote:my-bucket/ns/pvc", logrus.StandardLogger())
	if err != nil {
		t.Fatalf("generateCryptSection() error: %v", err)
	}

	for _, want := range []string{"[encrypted]", "type = crypt", "remote = remote:my-bucket/ns/pvc", "password = "} {
		if !strings.Contains(section, want) {
			t.Errorf("output missing %q, got:\n%s", want, section)
		}
	}

	// Verify the password field is non-empty
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "password = ") {
			pw := strings.TrimPrefix(line, "password = ")
			if pw == "" {
				t.Error("password should not be empty")
			}
		}
	}
}

func TestCheckRclonePartialSuccess(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantErr bool
	}{
		{
			name:    "no stats in output",
			output:  "some random log output\nERROR: something broke\n",
			wantErr: true,
		},
		{
			name:    "zero files transferred",
			output:  "ERROR : permission denied\nTransferred:            0 / 5, 0%\n",
			wantErr: true,
		},
		{
			name: "permission error only — partial success",
			output: "ERROR : .mongodb: failed to open directory \".mongodb\": open /data/.mongodb: permission denied\n" +
				"Transferred:           22 / 22, 100%\n",
			wantErr: false,
		},
		{
			name:    "all files transferred no errors — should not reach this func but handle gracefully",
			output:  "Transferred:           10 / 10, 100%\n",
			wantErr: true, // no ERROR but pod failed — genuine failure
		},
		{
			name: "non-permission error",
			output: "ERROR : network timeout\n" +
				"Transferred:            5 / 10, 50%\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkRclonePartialSuccess(tt.output, "test-pod", "test-ns", logrus.StandardLogger())
			if (err != nil) != tt.wantErr {
				t.Errorf("checkRclonePartialSuccess() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func makeValidateCmd(srcNS, destNS string) *TransferPVCCommand {
	return &TransferPVCCommand{
		Flags: Flags{
			PVC: PvcFlags{
				Name:      mappedNameVar{source: "my-pvc", destination: "my-pvc"},
				Namespace: mappedNameVar{source: srcNS, destination: destNS},
			},
		},
	}
}

func rcloneSecret(name, namespace string, confData []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data:       map[string][]byte{"rclone.conf": confData},
	}
}

func TestValidateRcloneConfigSecret(t *testing.T) {
	scheme := newTestScheme()
	validConf := []byte("[my-remote]\ntype = s3\n")

	tests := []struct {
		name     string
		srcObjs  []runtime.Object
		destObjs []runtime.Object
		wantErr  bool
		wantMsg  string
	}{
		{
			name:     "both clusters have a valid secret",
			srcObjs:  []runtime.Object{rcloneSecret("my-secret", "src-ns", validConf)},
			destObjs: []runtime.Object{rcloneSecret("my-secret", "dest-ns", validConf)},
			wantErr:  false,
		},
		{
			name:     "secret missing on source cluster",
			srcObjs:  []runtime.Object{},
			destObjs: []runtime.Object{rcloneSecret("my-secret", "dest-ns", validConf)},
			wantErr:  true,
			wantMsg:  "not found",
		},
		{
			name:     "secret missing on destination cluster",
			srcObjs:  []runtime.Object{rcloneSecret("my-secret", "src-ns", validConf)},
			destObjs: []runtime.Object{},
			wantErr:  true,
			wantMsg:  "not found",
		},
		{
			name:     "source secret has empty rclone.conf value",
			srcObjs:  []runtime.Object{rcloneSecret("my-secret", "src-ns", []byte{})},
			destObjs: []runtime.Object{rcloneSecret("my-secret", "dest-ns", validConf)},
			wantErr:  true,
			wantMsg:  "missing",
		},
		{
			name:     "destination secret has empty rclone.conf value",
			srcObjs:  []runtime.Object{rcloneSecret("my-secret", "src-ns", validConf)},
			destObjs: []runtime.Object{rcloneSecret("my-secret", "dest-ns", []byte{})},
			wantErr:  true,
			wantMsg:  "missing",
		},
		{
			name: "source secret has wrong key name instead of rclone.conf",
			srcObjs: []runtime.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "src-ns"},
				Data:       map[string][]byte{"rclone-config": validConf},
			}},
			destObjs: []runtime.Object{rcloneSecret("my-secret", "dest-ns", validConf)},
			wantErr:  true,
			wantMsg:  "missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := makeValidateCmd("src-ns", "dest-ns")
			srcClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.srcObjs...).Build()
			destClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.destObjs...).Build()

			err := cmd.validateRcloneConfigSecret("my-secret", srcClient, destClient)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRcloneConfigSecret() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("validateRcloneConfigSecret() error = %q, want to contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

// rcloneReveal decodes an rclone-obscured value for testing round-trips.
func rcloneReveal(obscured string) (string, error) {
	key := []byte{
		0x9c, 0x93, 0x5b, 0x48, 0x73, 0x0a, 0x55, 0x4d,
		0x6b, 0xfd, 0x7c, 0x63, 0xc8, 0x86, 0xa9, 0x2b,
		0xd3, 0x90, 0x19, 0x8e, 0xb8, 0x12, 0x8a, 0xfb,
		0xf4, 0xde, 0x16, 0x2b, 0x8b, 0x95, 0xf6, 0x38,
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(obscured)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	buf := ciphertext[aes.BlockSize:]
	iv := ciphertext[:aes.BlockSize]
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(buf, buf)
	return string(buf), nil
}
