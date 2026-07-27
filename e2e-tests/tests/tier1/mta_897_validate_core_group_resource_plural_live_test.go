package e2e

import (
	"log"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	cranevalidate "github.com/konveyor/crane/internal/validate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Crane validate: resourcePlural for core group resources (v1) [Live Mode]", func() {
	It("[MTA-897] should report correct resourcePlural for Pod, Namespace, PVC, and ServiceAccount",
		Label("tier1", "validate"), func() {
			testName := "validate-core-v1-plural"
			scenario := NewMigrationScenario(
				"core-v1-plural",
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
			paths, err := NewScenarioPaths("crane-validate-core-v1-plural-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				Expect(os.RemoveAll(paths.TempDir)).To(Succeed())
			})
			runner.WorkDir = paths.TempDir

			inputDir := filepath.Join(paths.TempDir, "input")
			Expect(os.MkdirAll(inputDir, 0o755)).NotTo(HaveOccurred())

			By("Copy testdata manifests to input directory")
			testdataFiles := []string{
				"test-897-pod.yaml",
				"test-897-namespace.yaml",
				"test-897-pvc.yaml",
				"test-897-serviceaccount.yaml",
			}
			for _, filename := range testdataFiles {
				sourcePath, err := filepath.Abs(filepath.Join("../../testdata/test-897", filename))
				Expect(err).NotTo(HaveOccurred())
				Expect(sourcePath).To(BeAnExistingFile(), "%s should exist in testdata/test-897", filename)

				data, err := os.ReadFile(sourcePath)
				Expect(err).NotTo(HaveOccurred())

				destPath := filepath.Join(inputDir, filename)
				Expect(os.WriteFile(destPath, data, 0o644)).NotTo(HaveOccurred())
			}

			By("Run crane validate in live mode")
			stdout, err := runner.Validate(ValidateOptions{
				Context:     scenario.KubectlTgt.Context,
				InputDir:    inputDir,
				ValidateDir: paths.ValidateDir,
			})
			Expect(err).NotTo(HaveOccurred(), "crane validate should succeed for core v1 resources")
			log.Printf("Validate stdout: %s", stdout)

			By("Parse validation report")
			var report cranevalidate.ValidationReport
			err = utils.ParseValidationReport(paths.ValidateDir, "json", &report)
			Expect(err).NotTo(HaveOccurred(), "should parse JSON report")

			By("Verify report using VerifyValidateResults")
			expectations := utils.ValidationExpectations{
				ValidationReport: cranevalidate.ValidationReport{
					Mode:           "live",
					ClusterContext: scenario.KubectlTgt.Context,
					TotalScanned:   4,
					Compatible:     4,
					Incompatible:   0,
				},
				ExpectedResources: map[string]string{
					"Pod":                   "v1",
					"Namespace":             "v1",
					"PersistentVolumeClaim": "v1",
					"ServiceAccount":        "v1",
				},
				ExpectedResourcePlurals: map[string]string{
					"Pod":                   "pods",
					"Namespace":             "namespaces",
					"PersistentVolumeClaim": "persistentvolumeclaims",
					"ServiceAccount":        "serviceaccounts",
				},
				ExpectedStatus:    cranevalidate.StatusOK,
				ExpectFailuresDir: false,
			}
			utils.VerifyValidateResults(report, paths.ValidateDir, "JSON", expectations)

			By("Verify namespace: empty for cluster-scoped Namespace, set for namespaced resources")
			namespace := testName
			for _, result := range report.Results {
				switch result.Kind {
				case "Namespace":
					Expect(result.Namespace).To(BeEmpty(),
						"expected Namespace to have empty namespace (cluster-scoped)")
				case "Pod", "PersistentVolumeClaim", "ServiceAccount":
					Expect(result.Namespace).To(Equal(namespace),
						"expected %s to be in namespace %s", result.Kind, namespace)
				}
			}
		})
})
