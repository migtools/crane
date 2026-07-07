package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	cranevalidate "github.com/konveyor/crane/internal/validate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Validate scanner nested directory behavior [Live Mode]", func() {
	It("[MTA-XXX] should scan nested output/resources/ns1, output/resources/ns2, and output/resources/_cluster directories", Label("tier1", "validate", "admin"), func() {
		testName := "validate-scanner-nested-dirs"
		scenario := NewMigrationScenario(
			"scanner-nested-validate-live",
			testName,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)

		if scenario.KubectlTgt.Context == "" {
			Skip("target-context is required")
		}

		runner := scenario.Crane
		paths, err := NewScenarioPaths("crane-validate-scanner-nested-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(os.RemoveAll(paths.TempDir)).To(Succeed())
		})
		runner.WorkDir = paths.TempDir

		inputDir := filepath.Join(paths.TempDir, "output")
		ns1Dir := filepath.Join(inputDir, "resources", "ns1")
		ns2Dir := filepath.Join(inputDir, "resources", "ns2")
		clusterDir := filepath.Join(inputDir, "resources", "_cluster")

		Expect(os.MkdirAll(ns1Dir, 0o755)).NotTo(HaveOccurred())
		Expect(os.MkdirAll(ns2Dir, 0o755)).NotTo(HaveOccurred())
		Expect(os.MkdirAll(clusterDir, 0o755)).NotTo(HaveOccurred())

		type manifestFile struct {
			relativePath string
			content      string
		}

		manifests := []manifestFile{
			{
				relativePath: "resources/ns1/deployment.yaml",
				content: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: ns1
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
          image: nginx
`,
			},
			{
				relativePath: "resources/ns1/service.yaml",
				content: `apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: ns1
spec:
  selector:
    app: web
  ports:
    - port: 80
      targetPort: 80
`,
			},
			{
				relativePath: "resources/ns1/configmap.yaml",
				content: `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: ns1
data:
  key: value
`,
			},
			{
				relativePath: "resources/ns2/secret.yaml",
				content: `apiVersion: v1
kind: Secret
metadata:
  name: app-secret
  namespace: ns2
type: Opaque
stringData:
  token: test-token
`,
			},
			{
				relativePath: "resources/ns2/serviceaccount.yaml",
				content: `apiVersion: v1
kind: ServiceAccount
metadata:
  name: app-sa
  namespace: ns2
`,
			},
			{
				relativePath: "resources/_cluster/clusterrole.yaml",
				content: `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: scanner-nested-reader
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
`,
			},
			{
				relativePath: "resources/_cluster/clusterrolebinding.yaml",
				content: `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: scanner-nested-reader-binding
subjects:
  - kind: ServiceAccount
    name: app-sa
    namespace: ns2
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: scanner-nested-reader
`,
			},
		}

		for _, mf := range manifests {
			destPath := filepath.Join(inputDir, mf.relativePath)
			Expect(os.WriteFile(destPath, []byte(mf.content), 0o644)).NotTo(HaveOccurred(), "write fixture %s", mf.relativePath)
		}

		stdout, err := runner.Validate(
			ValidateOptions{
				Context:      scenario.KubectlTgt.Context,
				InputDir:     inputDir,
				ValidateDir:  paths.ValidateDir,
				OutputFormat: "json",
			},
		)
		Expect(err).NotTo(HaveOccurred(), "validate should pass for nested compatible resources")
		Expect(stdout).To(ContainSubstring("Mode: live"))
		Expect(stdout).To(ContainSubstring("Result: PASSED"))

		By("Verify CLI table output namespace column for each expected row")
		assertNamespaceRow := func(apiVersion, kind, namespace, resourcePlural string) {
			var row string
			for _, line := range strings.Split(stdout, "\n") {
				if strings.Contains(line, apiVersion) && strings.Contains(line, kind) {
					row = line
					break
				}
			}
			Expect(row).NotTo(BeEmpty(), "expected stdout row for %s %s", apiVersion, kind)

			fields := strings.Fields(row)
			if namespace == "" {
				Expect(fields).To(HaveLen(4), "expected empty namespace row shape [apiVersion kind resource status] for %s", kind)
				Expect(fields[0]).To(Equal(apiVersion))
				Expect(fields[1]).To(Equal(kind))
				Expect(fields[2]).To(Equal(resourcePlural))
				Expect(fields[3]).To(Equal(string(cranevalidate.StatusOK)))
				return
			}
			Expect(fields).To(HaveLen(5), "expected namespaced row shape [apiVersion kind namespace resource status] for %s", kind)
			Expect(fields[0]).To(Equal(apiVersion))
			Expect(fields[1]).To(Equal(kind))
			Expect(fields[2]).To(Equal(namespace))
			Expect(fields[3]).To(Equal(resourcePlural))
			Expect(fields[4]).To(Equal(string(cranevalidate.StatusOK)))
		}

		type expectedStdoutRow struct {
			apiVersion     string
			kind           string
			namespace      string
			resourcePlural string
		}
		expectedRows := []expectedStdoutRow{
			{apiVersion: "apps/v1", kind: "Deployment", namespace: "ns1", resourcePlural: "deployments"},
			{apiVersion: "v1", kind: "Service", namespace: "ns1", resourcePlural: "services"},
			{apiVersion: "v1", kind: "ConfigMap", namespace: "ns1", resourcePlural: "configmaps"},
			{apiVersion: "v1", kind: "Secret", namespace: "ns2", resourcePlural: "secrets"},
			{apiVersion: "v1", kind: "ServiceAccount", namespace: "ns2", resourcePlural: "serviceaccounts"},
			{apiVersion: "rbac.authorization.k8s.io/v1", kind: "ClusterRole", namespace: "", resourcePlural: "clusterroles"},
			{apiVersion: "rbac.authorization.k8s.io/v1", kind: "ClusterRoleBinding", namespace: "", resourcePlural: "clusterrolebindings"},
		}
		for _, expectedRow := range expectedRows {
			assertNamespaceRow(expectedRow.apiVersion, expectedRow.kind, expectedRow.namespace, expectedRow.resourcePlural)
		}

		reportPath := filepath.Join(paths.ValidateDir, "report.json")
		Expect(reportPath).To(BeAnExistingFile())

		reportBytes, err := os.ReadFile(reportPath)
		Expect(err).NotTo(HaveOccurred())

		var report cranevalidate.ValidationReport
		Expect(json.Unmarshal(reportBytes, &report)).To(Succeed())

		Expect(report.Mode).To(Equal("live"))
		Expect(report.ClusterContext).To(Equal(scenario.KubectlTgt.Context))
		Expect(report.TotalScanned).To(Equal(7), "expected exactly 7 resources scanned from nested directories")
		Expect(report.Compatible).To(Equal(7), "expected all nested test resources to be compatible")
		Expect(report.Incompatible).To(Equal(0))
		Expect(report.Results).To(HaveLen(7))

		expectedKinds := map[string]string{
			"Deployment":         "ns1",
			"Service":            "ns1",
			"ConfigMap":          "ns1",
			"Secret":             "ns2",
			"ServiceAccount":     "ns2",
			"ClusterRole":        "",
			"ClusterRoleBinding": "",
		}

		foundKinds := map[string]bool{}
		for _, result := range report.Results {
			expectedNamespace, expected := expectedKinds[result.Kind]
			if !expected {
				continue
			}
			foundKinds[result.Kind] = true
			Expect(result.Status).To(Equal(cranevalidate.StatusOK), "expected %s to be compatible", result.Kind)
			Expect(result.Namespace).To(Equal(expectedNamespace), fmt.Sprintf("unexpected namespace for %s", result.Kind))
		}

		for kind := range expectedKinds {
			Expect(foundKinds[kind]).To(BeTrue(), "expected %s in validation results", kind)
		}
	})
})
