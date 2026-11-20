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

var _ = Describe("Audit logging invalid path fails command", func() {
	It("[MTA-919] crane transform fails before Run when audit log path cannot be created", Label("tier1"), func() {
		paths, err := NewScenarioPaths("crane-audit-invalid-*")
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

		// Place a regular file where a directory would need to be created so that
		// os.MkdirAll inside NewFileHook fails with "not a directory".
		blockFile := filepath.Join(paths.TempDir, "block")
		Expect(os.WriteFile(blockFile, []byte(""), 0644)).NotTo(HaveOccurred())
		badAuditLog := filepath.Join(blockFile, "subdir", "audit.log")

		By("Run crane transform with an audit log path that cannot be created (expect failure)")
		log.Printf("Running crane transform with bad audit log path: %s\n", badAuditLog)
		err = runner.Transform(TransformOptions{
			ExportDir:    testdataExportDir,
			TransformDir: paths.TransformDir,
			AuditLogPath: badAuditLog,
		})
		Expect(err).To(HaveOccurred(), "crane transform should fail when audit log path cannot be created")
		log.Printf("Transform failed as expected: %v\n", err)

		By("Verify error message mentions audit log")
		Expect(err.Error()).To(ContainSubstring("audit log"), "error output should contain 'audit log' indicating failure in Complete phase")

		By("Verify no transform output was produced")
		_, statErr := os.Stat(paths.TransformDir)
		Expect(os.IsNotExist(statErr)).To(BeTrue(), "transform dir should not be created — command stopped in Complete before Run")
	})
})
