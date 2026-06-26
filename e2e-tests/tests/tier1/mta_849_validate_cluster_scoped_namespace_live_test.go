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

var _ = Describe("Validate cluster-scoped namespace behavior [Live Mode]", func() {
	It("[MTA-849] should keep NAMESPACE empty for cluster-scoped resources in validate output", Label("tier1", "validate", "admin"), func() {
		testName := "validate-cluster-scoped-live"
		scenario := NewMigrationScenario(
			"cluster-scoped-validate-live",
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
		paths, err := NewScenarioPaths("crane-validate-cluster-scoped-live-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(os.RemoveAll(paths.TempDir)).To(Succeed())
		})
		runner.WorkDir = paths.TempDir

		inputDir := filepath.Join(paths.TempDir, "input")
		Expect(os.MkdirAll(inputDir, 0o755)).NotTo(HaveOccurred())

		clusterScopedManifest := fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: %s-role
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: %s-rolebinding
subjects:
  - kind: ServiceAccount
    name: default
    namespace: default
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: %s-role
---
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: %s-sc
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: WaitForFirstConsumer
`, testName, testName, testName, testName)

		manifestPath := filepath.Join(inputDir, "cluster-scoped.yaml")
		Expect(os.WriteFile(manifestPath, []byte(clusterScopedManifest), 0o644)).NotTo(HaveOccurred())

		stdout, err := runner.Validate(
			ValidateOptions{
				Context:     scenario.KubectlTgt.Context,
				InputDir:    inputDir,
				ValidateDir: paths.ValidateDir,
			},
		)
		Expect(err).NotTo(HaveOccurred(), "validate should pass for compatible cluster-scoped resources")
		Expect(stdout).To(ContainSubstring("Mode: live"))
		Expect(stdout).To(ContainSubstring("Result: PASSED"))
		By("Verify CLI table output keeps NAMESPACE column empty for cluster-scoped resources")
		assertEmptyNamespaceRow := func(apiVersion, kind, resourcePlural string) {
			var row string
			for _, line := range strings.Split(stdout, "\n") {
				if strings.Contains(line, apiVersion) && strings.Contains(line, kind) {
					row = line
					break
				}
			}
			Expect(row).NotTo(BeEmpty(), "expected stdout row for %s %s", apiVersion, kind)

			fields := strings.Fields(row)
			Expect(fields).To(HaveLen(4), "expected empty namespace row shape [apiVersion kind resource status] for %s", kind)
			Expect(fields[0]).To(Equal(apiVersion))
			Expect(fields[1]).To(Equal(kind))
			Expect(fields[2]).To(Equal(resourcePlural))
			Expect(fields[3]).To(Equal(string(cranevalidate.StatusOK)))
		}

		assertEmptyNamespaceRow("rbac.authorization.k8s.io/v1", "ClusterRole", "clusterroles")
		assertEmptyNamespaceRow("rbac.authorization.k8s.io/v1", "ClusterRoleBinding", "clusterrolebindings")
		assertEmptyNamespaceRow("storage.k8s.io/v1", "StorageClass", "storageclasses")

		reportPath := filepath.Join(paths.ValidateDir, "report.json")
		Expect(reportPath).To(BeAnExistingFile())

		reportBytes, err := os.ReadFile(reportPath)
		Expect(err).NotTo(HaveOccurred())

		var report cranevalidate.ValidationReport
		Expect(json.Unmarshal(reportBytes, &report)).To(Succeed())

		Expect(report.Mode).To(Equal("live"))
		Expect(report.ClusterContext).To(Equal(scenario.KubectlTgt.Context))
		Expect(report.TotalScanned).To(Equal(3))
		Expect(report.Compatible).To(Equal(3))
		Expect(report.Incompatible).To(Equal(0))
		Expect(report.Results).To(HaveLen(3))

		expectedKinds := map[string]string{
			"ClusterRole":        "rbac.authorization.k8s.io/v1",
			"ClusterRoleBinding": "rbac.authorization.k8s.io/v1",
			"StorageClass":       "storage.k8s.io/v1",
		}
		foundKinds := map[string]bool{}
		for _, result := range report.Results {
			expectedAPIVersion, expected := expectedKinds[result.Kind]
			if !expected {
				continue
			}

			foundKinds[result.Kind] = true
			Expect(result.APIVersion).To(Equal(expectedAPIVersion))
			Expect(result.Status).To(Equal(cranevalidate.StatusOK))
			Expect(result.Namespace).To(BeEmpty(), "expected namespace to be empty for cluster-scoped %s", result.Kind)
			Expect(result.ResourcePlural).NotTo(BeEmpty())
		}
		for kind := range expectedKinds {
			Expect(foundKinds[kind]).To(BeTrue(), "expected %s in validation results", kind)
		}

		failuresDir := filepath.Join(paths.ValidateDir, "failures")
		Expect(failuresDir).NotTo(BeADirectory())
	})
})
