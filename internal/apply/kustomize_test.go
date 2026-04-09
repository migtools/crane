package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestSplitMultiDocYAMLToFiles(t *testing.T) {
	tests := []struct {
		name           string
		yamlData       string
		expectedFiles  map[string]bool // path -> should exist
		expectedInFile map[string]string // file -> substring to check
		expectError    bool
	}{
		{
			name: "single deployment with namespace",
			yamlData: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 3
`,
			expectedFiles: map[string]bool{
				"resources/default/Deployment_default_myapp.yaml": true,
			},
			expectedInFile: map[string]string{
				"resources/default/Deployment_default_myapp.yaml": "replicas: 3",
			},
		},
		{
			name: "multiple resources",
			yamlData: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app1
  namespace: ns1
spec:
  replicas: 1
---
apiVersion: v1
kind: Service
metadata:
  name: svc1
  namespace: ns1
spec:
  type: ClusterIP
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: config1
  namespace: ns2
data:
  key: value
`,
			expectedFiles: map[string]bool{
				"resources/ns1/Deployment_ns1_app1.yaml": true,
				"resources/ns1/Service_ns1_svc1.yaml":    true,
				"resources/ns2/ConfigMap_ns2_config1.yaml": true,
			},
			expectedInFile: map[string]string{
				"resources/ns1/Deployment_ns1_app1.yaml": "replicas: 1",
				"resources/ns1/Service_ns1_svc1.yaml":    "type: ClusterIP",
				"resources/ns2/ConfigMap_ns2_config1.yaml": "key: value",
			},
		},
		{
			name: "cluster-scoped resource",
			yamlData: `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: admin
rules:
- apiGroups: ["*"]
  resources: ["*"]
  verbs: ["*"]
`,
			expectedFiles: map[string]bool{
				"resources/_cluster/ClusterRole_admin.yaml": true,
			},
			expectedInFile: map[string]string{
				"resources/_cluster/ClusterRole_admin.yaml": "verbs:",
			},
		},
		{
			name: "mixed namespaced and cluster-scoped",
			yamlData: `apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: prod
spec:
  type: LoadBalancer
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: reader
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]
`,
			expectedFiles: map[string]bool{
				"resources/prod/Service_prod_web.yaml": true,
				"resources/_cluster/ClusterRole_reader.yaml": true,
			},
			expectedInFile: map[string]string{
				"resources/prod/Service_prod_web.yaml": "type: LoadBalancer",
				"resources/_cluster/ClusterRole_reader.yaml": "verbs:",
			},
		},
		{
			name: "with init containers",
			yamlData: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: initcont
  namespace: test
spec:
  replicas: 1
  template:
    spec:
      initContainers:
      - name: init-db
        image: busybox
        command: ['sh', '-c', 'echo init']
      containers:
      - name: app
        image: nginx
`,
			expectedFiles: map[string]bool{
				"resources/test/Deployment_test_initcont.yaml": true,
			},
			expectedInFile: map[string]string{
				"resources/test/Deployment_test_initcont.yaml": "initContainers:",
			},
		},
		{
			name: "empty document handling",
			yamlData: `apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: default
spec:
  type: ClusterIP
---
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: default
data:
  key: val
`,
			expectedFiles: map[string]bool{
				"resources/default/Service_default_svc.yaml": true,
				"resources/default/ConfigMap_default_cm.yaml": true,
			},
			expectedInFile: map[string]string{
				"resources/default/Service_default_svc.yaml": "type: ClusterIP",
				"resources/default/ConfigMap_default_cm.yaml": "key: val",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			tmpDir, err := os.MkdirTemp("", "crane-split-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			// Create applier
			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel) // Quiet for tests
			applier := &KustomizeApplier{
				Log:       logger,
				OutputDir: tmpDir,
			}

			// Split YAML
			err = applier.splitMultiDocYAMLToFiles([]byte(tt.yamlData))

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Verify expected files exist
			for expectedPath, shouldExist := range tt.expectedFiles {
				fullPath := filepath.Join(tmpDir, expectedPath)
				_, err := os.Stat(fullPath)

				if shouldExist {
					if os.IsNotExist(err) {
						t.Errorf("Expected file %s does not exist", expectedPath)
					}
				} else {
					if err == nil {
						t.Errorf("Unexpected file %s exists", expectedPath)
					}
				}
			}

			// Verify file contents
			for filePath, expectedSubstring := range tt.expectedInFile {
				fullPath := filepath.Join(tmpDir, filePath)
				content, err := os.ReadFile(fullPath)
				if err != nil {
					t.Errorf("Failed to read %s: %v", filePath, err)
					continue
				}

				if !contains(string(content), expectedSubstring) {
					t.Errorf("File %s missing expected substring %q.\nContent:\n%s",
						filePath, expectedSubstring, string(content))
				}
			}
		})
	}
}

func TestSplitMultiDocYAMLToFilesEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		yamlData    string
		expectError bool
		description string
	}{
		{
			name:        "empty input",
			yamlData:    "",
			expectError: false,
			description: "Empty YAML should not error",
		},
		{
			name:        "only separators",
			yamlData:    "---\n---\n---",
			expectError: false,
			description: "Only separators should not error",
		},
		{
			name: "missing kind",
			yamlData: `apiVersion: v1
metadata:
  name: test
  namespace: default
`,
			expectError: true,
			description: "Resource without kind should error",
		},
		{
			name: "missing name",
			yamlData: `apiVersion: v1
kind: Service
metadata:
  namespace: default
`,
			expectError: false,
			description: "Resource without name should be skipped with warning",
		},
		{
			name: "special characters in name",
			yamlData: `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config-123
  namespace: kube-system
data:
  key: value
`,
			expectError: false,
			description: "Names with hyphens and numbers should work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "crane-edge-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel)
			applier := &KustomizeApplier{
				Log:       logger,
				OutputDir: tmpDir,
			}

			err = applier.splitMultiDocYAMLToFiles([]byte(tt.yamlData))

			if tt.expectError && err == nil {
				t.Errorf("%s: expected error but got none", tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("%s: unexpected error: %v", tt.description, err)
			}
		})
	}
}

func TestApplyFinalStageWithFileSplit(t *testing.T) {
	// This test verifies that ApplyFinalStage creates both output.yaml and individual files
	// We'll use a mock by creating a fake stage directory

	tmpDir, err := os.MkdirTemp("", "crane-apply-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a fake stage directory with kustomization
	transformDir := filepath.Join(tmpDir, "transform")
	stageDir := filepath.Join(transformDir, "10_test")
	resourcesDir := filepath.Join(stageDir, "resources")

	if err := os.MkdirAll(resourcesDir, 0700); err != nil {
		t.Fatalf("Failed to create stage dirs: %v", err)
	}

	// Write a simple resource
	resourceYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: default
data:
  key: value
`
	resourcePath := filepath.Join(resourcesDir, "configmap.yaml")
	if err := os.WriteFile(resourcePath, []byte(resourceYAML), 0644); err != nil {
		t.Fatalf("Failed to write resource: %v", err)
	}

	// Write kustomization.yaml
	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- resources/configmap.yaml
`
	kustomizationPath := filepath.Join(stageDir, "kustomization.yaml")
	if err := os.WriteFile(kustomizationPath, []byte(kustomizationYAML), 0644); err != nil {
		t.Fatalf("Failed to write kustomization: %v", err)
	}

	// Create applier
	outputDir := filepath.Join(tmpDir, "output")
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	applier := &KustomizeApplier{
		Log:          logger,
		TransformDir: transformDir,
		OutputDir:    outputDir,
	}

	// Run ApplyFinalStage
	err = applier.ApplyFinalStage()
	if err != nil {
		t.Fatalf("ApplyFinalStage failed: %v", err)
	}

	// Verify output.yaml exists
	outputYAMLPath := filepath.Join(outputDir, "output.yaml")
	if _, err := os.Stat(outputYAMLPath); os.IsNotExist(err) {
		t.Errorf("output.yaml not created")
	}

	// Verify individual file exists
	individualFilePath := filepath.Join(outputDir, "resources/default/ConfigMap_default_test-config.yaml")
	if _, err := os.Stat(individualFilePath); os.IsNotExist(err) {
		t.Errorf("Individual file not created: %s", individualFilePath)
	}

	// Verify content of individual file
	content, err := os.ReadFile(individualFilePath)
	if err != nil {
		t.Fatalf("Failed to read individual file: %v", err)
	}

	if !contains(string(content), "key: value") {
		t.Errorf("Individual file missing expected content")
	}
}

func TestValidateKubectlAvailable(t *testing.T) {
	// This test verifies that ValidateKubectlAvailable correctly checks for kubectl
	// We can't guarantee kubectl is available in all test environments,
	// so we just verify the function executes without panic

	err := ValidateKubectlAvailable()

	// If kubectl is available, err should be nil
	// If kubectl is not available, err should contain helpful message
	if err != nil {
		// Verify error message is helpful
		if !contains(err.Error(), "kubectl") {
			t.Errorf("Error message should mention kubectl, got: %v", err)
		}
		t.Logf("kubectl not available (expected in some test environments): %v", err)
	} else {
		t.Log("kubectl is available")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && findInString(s, substr)
}

func findInString(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
