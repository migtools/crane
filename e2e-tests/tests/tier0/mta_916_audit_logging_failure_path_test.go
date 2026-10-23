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

var _ = Describe("Audit logging failure path", func() {
	It("[MTA-916] audit log is written even when transform command fails", Label("tier0"), func() {
		paths, err := NewScenarioPaths("crane-audit-failure-*")
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Created temp directory: %s\n", paths.TempDir)

		DeferCleanup(func() {
			By("Cleanup temp directory")
			if err := os.RemoveAll(paths.TempDir); err != nil {
				log.Printf("cleanup: failed to remove temp dir: %v", err)
			}
		})

		runner := &CraneRunner{
			Bin:     config.CraneBin,
			WorkDir: paths.TempDir,
		}

		auditLogPath := filepath.Join(paths.TempDir, "failure-audit.log")
		nonExistentExportDir := filepath.Join(paths.TempDir, "does-not-exist")

		By("Run crane transform with non-existent export dir (expect failure)")
		log.Printf("Running crane transform with non-existent export dir: %s\n", nonExistentExportDir)
		err = runner.Transform(TransformOptions{
			ExportDir:    nonExistentExportDir,
			TransformDir: paths.TransformDir,
			AuditLogPath: auditLogPath,
		})
		Expect(err).To(HaveOccurred(), "crane transform should fail with non-existent export dir")
		log.Printf("Transform failed as expected: %v\n", err)

		By("Verify audit log file exists despite transform failure")
		Expect(auditLogPath).To(BeAnExistingFile())
		log.Printf("Audit log file exists: %s\n", auditLogPath)

		By("Verify at least one entry was written during the failed command")
		entries, err := utils.ReadAuditLogEntries(auditLogPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).NotTo(BeEmpty(), "audit log should contain entries even after command failure")
		log.Printf("Read %d entries from audit log\n", len(entries))
	})
})
