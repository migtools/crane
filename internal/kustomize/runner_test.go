package kustomize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestBuild_BasicKustomization(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kustomize-runner-basic-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	resourceYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: default
data:
  key: value
`
	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- configmap.yaml
`

	if err := os.WriteFile(filepath.Join(tmpDir, "configmap.yaml"), []byte(resourceYAML), 0644); err != nil {
		t.Fatalf("Failed to write resource: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "kustomization.yaml"), []byte(kustomizationYAML), 0644); err != nil {
		t.Fatalf("Failed to write kustomization: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	runner := &Runner{Log: logger}

	output, err := runner.Build(tmpDir)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "test-config") {
		t.Error("Output should contain ConfigMap name")
	}
	if !strings.Contains(outputStr, "key: value") {
		t.Error("Output should contain ConfigMap data")
	}
}

func TestBuild_WithPatches(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kustomize-runner-patch-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	resourceYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
      - name: web
        image: nginx:1.21
`
	patchYAML := `- op: replace
  path: /spec/replicas
  value: 3
`
	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- deployment.yaml
patches:
- path: patch.yaml
  target:
    group: apps
    version: v1
    kind: Deployment
    name: web
    namespace: default
`

	if err := os.WriteFile(filepath.Join(tmpDir, "deployment.yaml"), []byte(resourceYAML), 0644); err != nil {
		t.Fatalf("Failed to write resource: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "patch.yaml"), []byte(patchYAML), 0644); err != nil {
		t.Fatalf("Failed to write patch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "kustomization.yaml"), []byte(kustomizationYAML), 0644); err != nil {
		t.Fatalf("Failed to write kustomization: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	runner := &Runner{Log: logger}

	output, err := runner.Build(tmpDir)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if !strings.Contains(string(output), "replicas: 3") {
		t.Error("Patch should have changed replicas to 3")
	}
}

func TestBuild_WithLoadRestrictionsNone(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kustomize-runner-loadrestrict-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	resourceYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
  namespace: default
data:
  key: value
`
	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- configmap.yaml
`

	if err := os.WriteFile(filepath.Join(tmpDir, "configmap.yaml"), []byte(resourceYAML), 0644); err != nil {
		t.Fatalf("Failed to write resource: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "kustomization.yaml"), []byte(kustomizationYAML), 0644); err != nil {
		t.Fatalf("Failed to write kustomization: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	runner := &Runner{
		Log:  logger,
		Args: []string{"--load-restrictor=LoadRestrictionsNone"},
	}

	output, err := runner.Build(tmpDir)
	if err != nil {
		t.Fatalf("Build with --load-restrictor=LoadRestrictionsNone failed: %v", err)
	}

	if !strings.Contains(string(output), "test") {
		t.Error("Output should contain the ConfigMap")
	}
}

func TestBuild_InvalidDir(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	runner := &Runner{Log: logger}

	_, err := runner.Build("/nonexistent/path")
	if err == nil {
		t.Error("Build should fail for non-existent directory")
	}
}

func TestBuild_MixedNamespacedAndClusterScoped(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kustomize-runner-mixed-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	deployYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: my-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
      - name: web
        image: nginx:1.21
`
	clusterRoleYAML := `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: web-reader
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]
`
	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- deployment.yaml
- clusterrole.yaml
`

	if err := os.WriteFile(filepath.Join(tmpDir, "deployment.yaml"), []byte(deployYAML), 0644); err != nil {
		t.Fatalf("Failed to write deployment: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "clusterrole.yaml"), []byte(clusterRoleYAML), 0644); err != nil {
		t.Fatalf("Failed to write clusterrole: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "kustomization.yaml"), []byte(kustomizationYAML), 0644); err != nil {
		t.Fatalf("Failed to write kustomization: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	runner := &Runner{Log: logger}

	output, err := runner.Build(tmpDir)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Deployment") {
		t.Error("Output should contain Deployment")
	}
	if !strings.Contains(outputStr, "ClusterRole") {
		t.Error("Output should contain ClusterRole")
	}
}

func TestBuildOptions_ArgMapping(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		expectErr bool
	}{
		{
			name: "no args",
			args: nil,
		},
		{
			name: "enable-helm",
			args: []string{"--enable-helm"},
		},
		{
			name: "load-restrictor equals syntax",
			args: []string{"--load-restrictor=LoadRestrictionsNone"},
		},
		{
			name: "load-restrictor space syntax",
			args: []string{"--load-restrictor", "LoadRestrictionsNone"},
		},
		{
			name: "helm-command equals",
			args: []string{"--helm-command=helm3"},
		},
		{
			name: "helm-command space",
			args: []string{"--helm-command", "helm3"},
		},
		{
			name: "env var",
			args: []string{"--env", "MY_VAR=hello"},
		},
		{
			name: "env short flag",
			args: []string{"-e", "MY_VAR=hello"},
		},
		{
			name: "enable-alpha-plugins",
			args: []string{"--enable-alpha-plugins"},
		},
		{
			name:      "unknown arg",
			args:      []string{"--unknown-flag"},
			expectErr: true,
		},
		{
			name:      "load-restrictor missing value",
			args:      []string{"--load-restrictor"},
			expectErr: true,
		},
		{
			name:      "helm-command missing value",
			args:      []string{"--helm-command"},
			expectErr: true,
		},
		{
			name:      "env missing value",
			args:      []string{"--env"},
			expectErr: true,
		},
		{
			name:      "env invalid format",
			args:      []string{"--env", "NOEQUALS"},
			expectErr: true,
		},
		{
			name:      "invalid load-restrictor value",
			args:      []string{"--load-restrictor=InvalidValue"},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &Runner{Args: tt.args}
			_, _, err := runner.buildOptions()

			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}
