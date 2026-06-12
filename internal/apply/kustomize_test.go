package apply

import (
	"os"
	"path/filepath"
	"strings"
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
				"resources/default/340_Deployment_*_myapp.yaml": true,
			},
			expectedInFile: map[string]string{
				"resources/default/340_Deployment_*_myapp.yaml": "replicas: 3",
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
				"resources/ns1/340_Deployment_*_app1.yaml": true,
				"resources/ns1/400_Service_*_svc1.yaml":    true,
				"resources/ns2/240_ConfigMap_*_config1.yaml": true,
			},
			expectedInFile: map[string]string{
				"resources/ns1/340_Deployment_*_app1.yaml": "replicas: 1",
				"resources/ns1/400_Service_*_svc1.yaml":    "type: ClusterIP",
				"resources/ns2/240_ConfigMap_*_config1.yaml": "key: value",
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
				"resources/_cluster/050_ClusterRole_*_admin.yaml": true,
			},
			expectedInFile: map[string]string{
				"resources/_cluster/050_ClusterRole_*_admin.yaml": "verbs:",
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
				"resources/prod/400_Service_*_web.yaml": true,
				"resources/_cluster/050_ClusterRole_*_reader.yaml": true,
			},
			expectedInFile: map[string]string{
				"resources/prod/400_Service_*_web.yaml": "type: LoadBalancer",
				"resources/_cluster/050_ClusterRole_*_reader.yaml": "verbs:",
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
				"resources/test/340_Deployment_*_initcont.yaml": true,
			},
			expectedInFile: map[string]string{
				"resources/test/340_Deployment_*_initcont.yaml": "initContainers:",
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
				"resources/default/400_Service_*_svc.yaml": true,
				"resources/default/240_ConfigMap_*_cm.yaml": true,
			},
			expectedInFile: map[string]string{
				"resources/default/400_Service_*_svc.yaml": "type: ClusterIP",
				"resources/default/240_ConfigMap_*_cm.yaml": "key: val",
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

			// Verify expected files exist (expectedPath can contain wildcards)
			for expectedPath, shouldExist := range tt.expectedFiles {
				fullPattern := filepath.Join(tmpDir, expectedPath)
				matches, _ := filepath.Glob(fullPattern)

				if shouldExist {
					if len(matches) == 0 {
						t.Errorf("Expected file matching %s does not exist", expectedPath)
					}
				} else {
					if len(matches) > 0 {
						t.Errorf("Unexpected file matching %s exists: %v", expectedPath, matches)
					}
				}
			}

			// Verify file contents (filePath can contain wildcards)
			for filePath, expectedSubstring := range tt.expectedInFile {
				fullPattern := filepath.Join(tmpDir, filePath)
				matches, _ := filepath.Glob(fullPattern)

				if len(matches) == 0 {
					t.Errorf("File matching %s not found", filePath)
					continue
				}

				content, err := os.ReadFile(matches[0])
				if err != nil {
					t.Errorf("Failed to read %s: %v", matches[0], err)
					continue
				}

				if !contains(string(content), expectedSubstring) {
					t.Errorf("File %s missing expected substring %q.\nContent:\n%s",
						matches[0], expectedSubstring, string(content))
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


func TestSplitMultiDocYAMLToFiles_Indentation(t *testing.T) {
	// Verify that split YAML files use 2-space indentation
	tmpDir, err := os.MkdirTemp("", "crane-indent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	yamlData := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: default
data:
  key1: value1
  key2: value2
`

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	applier := &KustomizeApplier{
		Log:       logger,
		OutputDir: tmpDir,
	}

	err = applier.splitMultiDocYAMLToFiles([]byte(yamlData))
	if err != nil {
		t.Fatalf("splitMultiDocYAMLToFiles failed: %v", err)
	}

	// Read the generated file (using glob to find it with new naming)
	globPattern := filepath.Join(tmpDir, "resources/default/240_ConfigMap_*_test-config.yaml")
	matches, _ := filepath.Glob(globPattern)
	if len(matches) == 0 {
		t.Fatalf("No ConfigMap file found matching pattern: %s", globPattern)
	}

	content, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	// Check for 2-space indentation
	lines := strings.Split(string(content), "\n")
	foundIndentedLine := false
	for _, line := range lines {
		if len(line) > 0 && line[0] == ' ' {
			// Count leading spaces
			spaces := 0
			for _, ch := range line {
				if ch == ' ' {
					spaces++
				} else {
					break
				}
			}

			// Indentation should be multiple of 2
			if spaces%2 != 0 {
				t.Errorf("Found line with odd indentation (%d spaces): %q", spaces, line)
			}

			// Should not have 4-space indentation (would indicate 4-space indent setting)
			if spaces == 4 && strings.Contains(line, "name:") {
				// This would be nested one level under metadata, should be 2 spaces
				t.Errorf("Found 4-space indentation where 2 was expected: %q", line)
			}

			foundIndentedLine = true
		}
	}

	if !foundIndentedLine {
		t.Error("Expected to find indented lines in YAML output")
	}
}

func TestSplitMultiDocYAML_SkipClusterScoped(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crane-skip-cluster-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	yamlData := `apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: prod
spec:
  type: ClusterIP
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: reader
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: reader-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: reader
subjects:
- kind: ServiceAccount
  name: default
  namespace: prod
`

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	applier := &KustomizeApplier{
		Log:               logger,
		OutputDir:         tmpDir,
		SkipClusterScoped: true,
	}

	// Filter first, then split (mirrors ApplyMultiStage flow)
	filtered, err := applier.filterClusterScopedResources([]byte(yamlData))
	if err != nil {
		t.Fatalf("filterClusterScopedResources failed: %v", err)
	}

	err = applier.splitMultiDocYAMLToFiles(filtered)
	if err != nil {
		t.Fatalf("splitMultiDocYAMLToFiles failed: %v", err)
	}

	// Namespaced resource should exist
	svcMatches, _ := filepath.Glob(filepath.Join(tmpDir, "resources/prod/400_Service_*_web.yaml"))
	if len(svcMatches) == 0 {
		t.Error("Service should be in output when --skip-cluster-scoped is set")
	}

	// Cluster-scoped resources should NOT exist
	clusterDir := filepath.Join(tmpDir, "resources", "_cluster")
	if _, err := os.Stat(clusterDir); !os.IsNotExist(err) {
		t.Error("_cluster/ directory should NOT exist when --skip-cluster-scoped is set")
	}

	// Verify filtered output.yaml does not contain cluster-scoped resources
	if contains(string(filtered), "ClusterRole") {
		t.Error("Filtered YAML should not contain ClusterRole")
	}
	if !contains(string(filtered), "Service") {
		t.Error("Filtered YAML should still contain Service")
	}
}

func TestSplitMultiDocYAML_ClusterScopedIncluded(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crane-include-cluster-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	yamlData := `apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: prod
spec:
  type: ClusterIP
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: reader
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get"]
`

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	applier := &KustomizeApplier{
		Log:               logger,
		OutputDir:         tmpDir,
		SkipClusterScoped: false,
	}

	err = applier.splitMultiDocYAMLToFiles([]byte(yamlData))
	if err != nil {
		t.Fatalf("splitMultiDocYAMLToFiles failed: %v", err)
	}

	// Both should exist
	svcMatches, _ := filepath.Glob(filepath.Join(tmpDir, "resources/prod/400_Service_*_web.yaml"))
	if len(svcMatches) == 0 {
		t.Error("Service should be in output")
	}

	crMatches, _ := filepath.Glob(filepath.Join(tmpDir, "resources/_cluster/050_ClusterRole_*_reader.yaml"))
	if len(crMatches) == 0 {
		t.Error("ClusterRole should be in output when --skip-cluster-scoped is NOT set")
	}
}

func TestSplitMultiDocYAML_OnlyClusterScoped(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crane-only-cluster-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	yamlData := `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: admin
rules:
- apiGroups: ["*"]
  resources: ["*"]
  verbs: ["*"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: admin-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin
subjects:
- kind: Group
  name: admins
  apiGroup: rbac.authorization.k8s.io
`

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	applier := &KustomizeApplier{
		Log:               logger,
		OutputDir:         tmpDir,
		SkipClusterScoped: true,
	}

	filtered, err := applier.filterClusterScopedResources([]byte(yamlData))
	if err != nil {
		t.Fatalf("filterClusterScopedResources failed: %v", err)
	}

	// Filtered output should be empty
	if len(strings.TrimSpace(string(filtered))) != 0 {
		t.Errorf("Expected empty output when all resources are cluster-scoped and skip is set, got: %q", string(filtered))
	}

	// Splitting empty content should not error
	err = applier.splitMultiDocYAMLToFiles(filtered)
	if err != nil {
		t.Fatalf("splitMultiDocYAMLToFiles on empty input should not error: %v", err)
	}

	// No resources directory should be created
	resourcesDir := filepath.Join(tmpDir, "resources")
	if _, err := os.Stat(resourcesDir); !os.IsNotExist(err) {
		t.Error("resources/ directory should NOT exist when all resources are filtered out")
	}
}

func TestFilterClusterScopedResources_PreservesNamespaced(t *testing.T) {
	yamlData := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: my-app
spec:
  replicas: 1
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: test-sa
  namespace: my-app
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: web-reader
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get"]
`

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	applier := &KustomizeApplier{
		Log:               logger,
		SkipClusterScoped: true,
	}

	filtered, err := applier.filterClusterScopedResources([]byte(yamlData))
	if err != nil {
		t.Fatalf("filterClusterScopedResources failed: %v", err)
	}

	filteredStr := string(filtered)

	// Should keep namespaced resources
	if !contains(filteredStr, "Deployment") {
		t.Error("Filtered YAML should contain Deployment")
	}
	if !contains(filteredStr, "ServiceAccount") {
		t.Error("Filtered YAML should contain ServiceAccount")
	}

	// Should remove cluster-scoped
	if contains(filteredStr, "ClusterRole") {
		t.Error("Filtered YAML should NOT contain ClusterRole")
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
