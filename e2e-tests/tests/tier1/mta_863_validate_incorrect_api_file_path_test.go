package e2e

import (
	"log"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[MTA-863] Crane validate offline: incorrect API resources file path", Label("tier1", "validate"), func() {
	var (
		tempDir  string
		runner   CraneRunner
		inputDir string
	)

	BeforeEach(func() {
		var err error

		By("Create temporary directory for test")
		tempDir, err = os.MkdirTemp("", "crane-validate-incorrect-api-*")
		Expect(err).NotTo(HaveOccurred())

		runner = CraneRunner{
			Bin: config.CraneBin,
		}
		runner.WorkDir = tempDir

		By("Create input directory and copy static manifest")
		inputDir = filepath.Join(tempDir, "input")
		Expect(os.MkdirAll(inputDir, 0o755)).NotTo(HaveOccurred())

		// From tier1 directory, go up to testdata (../../testdata)
		sourcePath, err := filepath.Abs(filepath.Join("../../testdata", "test-850-duplicate-deployment-1.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(sourcePath).To(BeAnExistingFile(), "static manifest should exist in testdata")

		manifestData, err := os.ReadFile(sourcePath)
		Expect(err).NotTo(HaveOccurred())

		destPath := filepath.Join(inputDir, "deployment.yaml")
		Expect(os.WriteFile(destPath, manifestData, 0o644)).NotTo(HaveOccurred())
		log.Printf("Copied static manifest to %s", inputDir)
	})

	AfterEach(func() {
		By("Cleanup temporary directory")
		if err := os.RemoveAll(tempDir); err != nil {
			log.Printf("cleanup: failed to remove temp dir %q: %v", tempDir, err)
		}
	})

	DescribeTable("should fail gracefully with incorrect API resources file path",
		func(testCase string, getAPIFilePath func(string) string, expectedErrorSubstrings []string) {
			By("Attempt to run crane validate with " + testCase)
			apiFilePath := getAPIFilePath(tempDir)
			validateDir := filepath.Join(tempDir, "validate-"+testCase)

			stdout, err := runner.Validate(ValidateOptions{
				InputDir:         inputDir,
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
			func(tmpDir string) string {
				return filepath.Join(tmpDir, "non-existent-api-surface.json")
			},
			[]string{"no such file or directory"},
		),

		Entry("[MTA-863] provide directory path instead of file",
			"directory-path",
			func(tmpDir string) string {
				return tmpDir // Return directory path
			},
			[]string{"is a directory"},
		),
	)
})
