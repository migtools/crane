package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

// TestRoleRoleBindingOrdering verifies that Role resources are written before RoleBinding
// This is a regression test for issue #266
func TestRoleRoleBindingOrdering(t *testing.T) {
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "output")

	// Create a multi-doc YAML with RoleBinding and Role (intentionally in wrong order)
	yamlData := []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: pod-reader-binding
  namespace: test-ns
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: pod-reader
subjects:
- kind: ServiceAccount
  name: test-sa
  namespace: test-ns
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pod-reader
  namespace: test-ns
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch"]
`)

	applier := &KustomizeApplier{
		Log:       logrus.New(),
		OutputDir: outputDir,
		Ordered:   true, // Enable ordering for this test
	}

	// Split the multi-doc YAML
	err := applier.splitMultiDocYAMLToFiles(yamlData)
	if err != nil {
		t.Fatalf("Failed to split YAML: %v", err)
	}

	// List files in the output directory
	resourcesDir := filepath.Join(outputDir, "resources", "test-ns")
	files, err := os.ReadDir(resourcesDir)
	if err != nil {
		t.Fatalf("Failed to read resources directory: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("Expected 2 files, got %d", len(files))
	}

	// Get sorted filenames
	var filenames []string
	for _, f := range files {
		filenames = append(filenames, f.Name())
	}

	// First file should be Role (order 300)
	if !strings.HasPrefix(filenames[0], "300_Role_") {
		t.Errorf("First file should be Role, got: %s", filenames[0])
	}

	// Second file should be RoleBinding (order 310)
	if !strings.HasPrefix(filenames[1], "310_RoleBinding_") {
		t.Errorf("Second file should be RoleBinding, got: %s", filenames[1])
	}

	// Verify that when sorted alphabetically, Role comes before RoleBinding
	if filenames[0] >= filenames[1] {
		t.Errorf("Role filename (%s) should sort before RoleBinding filename (%s)",
			filenames[0], filenames[1])
	}
}

// TestConfigMapDeploymentOrdering verifies that ConfigMap is written before Deployment
func TestConfigMapDeploymentOrdering(t *testing.T) {
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "output")

	yamlData := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: test-ns
spec:
  replicas: 1
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
      - name: app
        image: nginx
        envFrom:
        - configMapRef:
            name: app-config
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: test-ns
data:
  key: value
`)

	applier := &KustomizeApplier{
		Log:       logrus.New(),
		OutputDir: outputDir,
		Ordered:   true, // Enable ordering for this test
	}

	err := applier.splitMultiDocYAMLToFiles(yamlData)
	if err != nil {
		t.Fatalf("Failed to split YAML: %v", err)
	}

	resourcesDir := filepath.Join(outputDir, "resources", "test-ns")
	files, err := os.ReadDir(resourcesDir)
	if err != nil {
		t.Fatalf("Failed to read resources directory: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("Expected 2 files, got %d", len(files))
	}

	var filenames []string
	for _, f := range files {
		filenames = append(filenames, f.Name())
	}

	// ConfigMap (240) should come before Deployment (340)
	if !strings.HasPrefix(filenames[0], "240_ConfigMap_") {
		t.Errorf("First file should be ConfigMap, got: %s", filenames[0])
	}

	if !strings.HasPrefix(filenames[1], "340_Deployment_") {
		t.Errorf("Second file should be Deployment, got: %s", filenames[1])
	}
}

// TestClusterScopedOrdering verifies ordering for cluster-scoped resources
func TestClusterScopedOrdering(t *testing.T) {
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "output")

	yamlData := []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: admin-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin-role
subjects:
- kind: ServiceAccount
  name: admin
  namespace: default
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: admin-role
rules:
- apiGroups: ["*"]
  resources: ["*"]
  verbs: ["*"]
`)

	applier := &KustomizeApplier{
		Log:       logrus.New(),
		OutputDir: outputDir,
		Ordered:   true, // Enable ordering for this test
	}

	err := applier.splitMultiDocYAMLToFiles(yamlData)
	if err != nil {
		t.Fatalf("Failed to split YAML: %v", err)
	}

	resourcesDir := filepath.Join(outputDir, "resources", "_cluster")
	files, err := os.ReadDir(resourcesDir)
	if err != nil {
		t.Fatalf("Failed to read resources directory: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("Expected 2 files, got %d", len(files))
	}

	var filenames []string
	for _, f := range files {
		filenames = append(filenames, f.Name())
	}

	// ClusterRole (50) should come before ClusterRoleBinding (60)
	if !strings.HasPrefix(filenames[0], "050_ClusterRole_") {
		t.Errorf("First file should be ClusterRole, got: %s", filenames[0])
	}

	if !strings.HasPrefix(filenames[1], "060_ClusterRoleBinding_") {
		t.Errorf("Second file should be ClusterRoleBinding, got: %s", filenames[1])
	}
}

// TestWebhookOrdering verifies that webhook configurations are applied after workloads
// This prevents bootstrap deadlocks where webhooks are registered before their backend is ready
func TestWebhookOrdering(t *testing.T) {
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "output")

	// Create YAML with webhook config and deployment (intentionally in wrong order)
	yamlData := []byte(`apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: my-webhook
webhooks:
- name: validate.example.com
  clientConfig:
    service:
      name: webhook-service
      namespace: default
      path: /validate
  rules:
  - operations: ["CREATE"]
    apiGroups: ["apps"]
    apiVersions: ["v1"]
    resources: ["deployments"]
---
apiVersion: v1
kind: Service
metadata:
  name: webhook-service
  namespace: default
spec:
  ports:
  - port: 443
    targetPort: 8443
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webhook-server
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: webhook
  template:
    metadata:
      labels:
        app: webhook
    spec:
      containers:
      - name: webhook
        image: webhook:latest
`)

	applier := &KustomizeApplier{
		Log:       logrus.New(),
		OutputDir: outputDir,
		Ordered:   true, // Enable ordering for this test
	}

	err := applier.splitMultiDocYAMLToFiles(yamlData)
	if err != nil {
		t.Fatalf("Failed to split YAML: %v", err)
	}

	resourcesDir := filepath.Join(outputDir, "resources", "default")
	files, err := os.ReadDir(resourcesDir)
	if err != nil {
		t.Fatalf("Failed to read resources directory: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("Expected 2 files (Service and Deployment), got %d", len(files))
	}

	var filenames []string
	for _, f := range files {
		filenames = append(filenames, f.Name())
	}

	if !strings.HasPrefix(filenames[0], "340_Deployment_") {
		t.Errorf("First file should be Deployment (340), got: %s", filenames[0])
	}

	if !strings.HasPrefix(filenames[1], "400_Service_") {
		t.Errorf("Second file should be Service (400), got: %s", filenames[1])
	}

	// Check cluster-scoped webhook configuration
	clusterDir := filepath.Join(outputDir, "resources", "_cluster")
	clusterFiles, err := os.ReadDir(clusterDir)
	if err != nil {
		t.Fatalf("Failed to read cluster resources directory: %v", err)
	}

	if len(clusterFiles) != 1 {
		t.Fatalf("Expected 1 cluster file (ValidatingWebhookConfiguration), got %d", len(clusterFiles))
	}

	webhookFilename := clusterFiles[0].Name()

	// ValidatingWebhookConfiguration (800) should have high order number
	if !strings.HasPrefix(webhookFilename, "800_ValidatingWebhookConfiguration_") {
		t.Errorf("Webhook file should be ValidatingWebhookConfiguration with order 800, got: %s", webhookFilename)
	}

	// Most importantly: webhook file (800_*) should sort AFTER workload files (340_*, 400_*)
	if webhookFilename < filenames[0] || webhookFilename < filenames[1] {
		t.Errorf("Webhook file (%s) should sort after workload files (%v) to prevent bootstrap deadlock",
			webhookFilename, filenames)
	}
}

