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

var _ = Describe("Crane validate: verify resourcePlural for core group resources (v1) [Live Mode]", func() {
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

			type resourceCase struct {
				testdataFile   string
				kind           string
				apiVersion     string
				resourcePlural string
				namespace      string
			}
			namespace := testName
			resources := []resourceCase{
				{"test-897-pod.yaml", "Pod", "v1", "pods", namespace},
				{"test-897-namespace.yaml", "Namespace", "v1", "namespaces", ""},
				{"test-897-pvc.yaml", "PersistentVolumeClaim", "v1", "persistentvolumeclaims", namespace},
				{"test-897-serviceaccount.yaml", "ServiceAccount", "v1", "serviceaccounts", namespace},
			}

			inputDir := filepath.Join(paths.TempDir, "input")
			Expect(os.MkdirAll(inputDir, 0o755)).NotTo(HaveOccurred())

			By("Copy testdata manifests to input directory")
			for _, rc := range resources {
				sourcePath, err := filepath.Abs(filepath.Join("../../testdata/test-897", rc.testdataFile))
				Expect(err).NotTo(HaveOccurred())
				Expect(sourcePath).To(BeAnExistingFile(), "%s should exist in testdata/test-897", rc.testdataFile)

				data, err := os.ReadFile(sourcePath)
				Expect(err).NotTo(HaveOccurred())

				destPath := filepath.Join(inputDir, rc.testdataFile)
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

			expectedResources := make(map[string]string, len(resources))
			expectedPlurals := make(map[string]string, len(resources))
			for _, rc := range resources {
				expectedResources[rc.kind] = rc.apiVersion
				expectedPlurals[rc.kind] = rc.resourcePlural
			}

			By("Verify report using VerifyValidateResults")
			expectations := utils.ValidationExpectations{
				ValidationReport: cranevalidate.ValidationReport{
					Mode:           "live",
					ClusterContext: scenario.KubectlTgt.Context,
					TotalScanned:   len(resources),
					Compatible:     len(resources),
					Incompatible:   0,
				},
				ExpectedResources:       expectedResources,
				ExpectedResourcePlurals: expectedPlurals,
				ExpectedStatus:          cranevalidate.StatusOK,
				ExpectFailuresDir:       false,
			}
			utils.VerifyValidateResults(report, paths.ValidateDir, "JSON", expectations)

			By("Verify namespace: empty for cluster-scoped, set for namespaced resources")
			expectedNamespaces := make(map[string]string, len(resources))
			for _, rc := range resources {
				expectedNamespaces[rc.kind] = rc.namespace
			}
			for _, result := range report.Results {
				if expectedNs, ok := expectedNamespaces[result.Kind]; ok {
					Expect(result.Namespace).To(Equal(expectedNs),
						"expected %s to have namespace %q", result.Kind, expectedNs)
				}
			}
		})
})
