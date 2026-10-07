package e2e

import (
	"log"
	"os"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Audit logging /dev/null support", func() {
	It("[MTA-920] crane transform succeeds and discards entries when --audit-log /dev/null", Label("tier1"), func() {
		paths, err := NewScenarioPaths("crane-audit-devnull-*")
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Created temp directory: %s\n", paths.TempDir)

		DeferCleanup(func() {
			By("Cleanup temp directory")
			if err := os.RemoveAll(paths.TempDir); err != nil {
				log.Printf("cleanup: failed to remove temp dir: %v", err)
			}
		})

		testdataExportDir, err := utils.TestdataFilePath("audit-log-export")
		Expect(err).NotTo(HaveOccurred())

		runner := &CraneRunner{
			Bin:     config.CraneBin,
			WorkDir: paths.TempDir,
		}

		By("Run crane transform with --audit-log /dev/null")
		log.Printf("Running crane transform with --audit-log /dev/null from %s\n", testdataExportDir)
		consoleOutput, err := runner.TransformWithOutput(TransformOptions{
			ExportDir:    testdataExportDir,
			TransformDir: paths.TransformDir,
			AuditLogPath: "/dev/null",
		})
		Expect(err).NotTo(HaveOccurred(), "crane transform should succeed with --audit-log /dev/null")
		log.Printf("Transform completed successfully\n")

		By("Verify no audit log warning in console output")
		log.Printf("Console output length: %d bytes\n", len(consoleOutput))
		Expect(consoleOutput).NotTo(ContainSubstring("Failed to open audit log"), "output should not warn about audit log failure")

		By("Verify transform output was produced (command ran to completion)")
		hasFiles, _, err := utils.HasFilesRecursively(paths.TransformDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasFiles).To(BeTrue(), "transform dir should contain output files — command should have run to completion")
	})
})
