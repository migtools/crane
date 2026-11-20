package e2e

import (
	"log"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("FileHook captures debug entries; ConsoleHook suppresses them without --debug", func() {
	It("[MTA-918] debug entries appear in audit log file but not in console output", Label("tier1"), func() {
		paths, err := NewScenarioPaths("crane-audit-debug-*")
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
		auditLogPath := filepath.Join(paths.TempDir, "debug-audit.log")

		By("Run crane transform without --debug flag")
		log.Printf("Running crane transform without --debug from %s\n", testdataExportDir)
		consoleOutput, err := runner.TransformWithOutput(TransformOptions{
			ExportDir:    testdataExportDir,
			TransformDir: paths.TransformDir,
			AuditLogPath: auditLogPath,
		})
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Transform completed successfully\n")

		By("Read and parse audit log entries")
		entries, err := utils.ReadAuditLogEntries(auditLogPath)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Read %d entries from audit log\n", len(entries))

		By("Verify audit log contains at least one debug entry")
		hasDebug := false
		for _, entry := range entries {
			if level, ok := entry["level"].(string); ok && level == "debug" {
				hasDebug = true
				break
			}
		}
		Expect(hasDebug).To(BeTrue(), "audit log should contain at least one debug entry even without --debug flag")

		By("Verify console output does not contain debug entries")
		log.Printf("Console output length: %d bytes\n", len(consoleOutput))
		Expect(consoleOutput).NotTo(ContainSubstring("level=debug"), "ConsoleHook should suppress debug entries when --debug is not set")
	})
})
