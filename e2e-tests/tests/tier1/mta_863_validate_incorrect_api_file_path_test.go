package e2e

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[MTA-863] Crane validate offline: incorrect API resources file path", Label("tier1", "validate"), func() {
	var (
		namespace   string
		kubectlSrc  KubectlRunner
		runner      CraneRunner
		paths       ScenarioPaths
		cleanupDone bool
	)

	BeforeEach(func() {
		namespace = "test-validate-incorrect-api"
		kubectlSrc = KubectlRunner{Context: config.SourceContext}
		runner = CraneRunner{
			Bin:           config.CraneBin,
			SourceContext: config.SourceContext,
		}
		cleanupDone = false

		By("Create test namespace")
		Expect(kubectlSrc.CreateNamespace(namespace)).NotTo(HaveOccurred())

		By("Apply static test manifest to namespace")
		staticManifest := filepath.Join("e2e-tests", "testdata", "test-850-duplicate-deployment-1.yaml")
		manifestContent, err := os.ReadFile(staticManifest)
		Expect(err).NotTo(HaveOccurred(), "should read static manifest file")

		// Replace test-namespace with our actual namespace
		manifestYAML := strings.ReplaceAll(string(manifestContent), "namespace: test-namespace", "namespace: "+namespace)
		Expect(kubectlSrc.ApplyYAMLSpec(manifestYAML, namespace)).NotTo(HaveOccurred())
		log.Printf("Applied static test manifest to namespace %s", namespace)

		By("Setup temporary directories for crane pipeline")
		paths, err = NewScenarioPaths("crane-validate-incorrect-api-*")
		Expect(err).NotTo(HaveOccurred())

		By("Run crane export/transform/apply pipeline")
		runner.WorkDir = paths.TempDir
		exportOpts := ExportOptions{Namespace: namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{
			ExportDir:    paths.ExportDir,
			TransformDir: paths.TransformDir,
			OutputDir:    paths.OutputDir,
		}

		log.Printf("Running crane pipeline for namespace %s", namespace)
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed - output ready for validation tests")
	})

	AfterEach(func() {
		if !cleanupDone {
			By("Delete test namespace")
			if _, err := kubectlSrc.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
				log.Printf("cleanup: failed to delete namespace %q: %v", namespace, err)
			}

			By("Cleanup temporary directories")
			if err := os.RemoveAll(paths.TempDir); err != nil {
				log.Printf("cleanup: failed to remove temp dir %q: %v", paths.TempDir, err)
			}
			cleanupDone = true
		}
	})

	DescribeTable("should fail gracefully with incorrect API resources file path",
		func(testCase string, getAPIFilePath func(ScenarioPaths) string, expectedErrorSubstrings []string) {
			By("Attempt to run crane validate with " + testCase)
			apiFilePath := getAPIFilePath(paths)
			validateDir := filepath.Join(paths.TempDir, "validate-"+testCase)

			stdout, err := runner.Validate(ValidateOptions{
				InputDir:         paths.OutputDir,
				ValidateDir:      validateDir,
				APIResourcesFile: apiFilePath,
			})

			By("Verify crane validate fails with expected error")
			Expect(err).To(HaveOccurred(), "crane validate should fail when "+testCase)
			log.Printf("Test case '%s' - Expected failure: %v", testCase, err)
			log.Printf("Test case '%s' - Output: %s", testCase, stdout)

			By("Verify error message contains expected substrings")
			errMsg := err.Error()
			for _, expectedSubstring := range expectedErrorSubstrings {
				Expect(errMsg).To(ContainSubstring(expectedSubstring),
					"error message should contain '%s'", expectedSubstring)
			}
			log.Printf("Test case '%s' - All expected error substrings verified", testCase)
		},

		Entry("[MTA-863] non-existent file path",
			"non-existent-file",
			func(p ScenarioPaths) string {
				return filepath.Join(p.TempDir, "non-existent-api-surface.json")
			},
			[]string{"no such file or directory"},
		),

		Entry("[MTA-863] directory path instead of file",
			"directory-path",
			func(p ScenarioPaths) string {
				return p.TempDir // Return directory path
			},
			[]string{"is a directory"},
		),
	)
})
