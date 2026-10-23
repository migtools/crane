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

var _ = Describe("Custom paths work: simple and nested directories with auto-creation", func() {
	It("[MTA-917] custom --audit-log path is used; default path is not created", Label("tier1"), func() {
		By("Case 1: simple custom path")
		paths, err := NewScenarioPaths("crane-audit-custom-*")
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
		customAuditPath := filepath.Join(paths.TempDir, "my-custom-audit.log")

		transformOpts := TransformOptions{
			ExportDir:    testdataExportDir,
			TransformDir: paths.TransformDir,
			AuditLogPath: customAuditPath,
		}

		By("Run crane transform (offline)")
		log.Printf("Running crane transform from %s to %s\n", testdataExportDir, paths.TransformDir)
		Expect(runner.Transform(transformOpts)).NotTo(HaveOccurred())
		log.Printf("Transform completed successfully\n")

		defaultAuditLog := filepath.Join(paths.TempDir, "audit", ".crane-audit.log")

		By("Verify audit log file does not exist at default path")
		Expect(defaultAuditLog).NotTo(BeAnExistingFile())
		log.Printf("Audit log file does not exist at default path: %s\n", defaultAuditLog)

		By("Verify custom file exists")
		Expect(customAuditPath).To(BeAnExistingFile())
		log.Printf("Audit log file exists in custom path: %s\n", customAuditPath)

		By("Read and parse audit log entries")
		entries, err := utils.ReadAuditLogEntries(customAuditPath)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Read %d entries from audit log\n", len(entries))

		By("Verify entries are not empty")
		Expect(entries).NotTo(BeEmpty(), "audit log should contain entries")

		By("Case 2: nested path with auto-creation")
		paths2, err := NewScenarioPaths("crane-audit-nested-*")
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Created temp directory: %s\n", paths2.TempDir)

		DeferCleanup(func() {
			By("Cleanup temp directory")
			if err := os.RemoveAll(paths2.TempDir); err != nil {
				log.Printf("cleanup: failed to remove temp dir: %v", err)
			}
		})

		nestedAuditLog := filepath.Join(paths2.TempDir, "new", "nested", "subdir", "audit.log")

		By("Verify nested parent dir does not exist before transform")
		_, err = os.Stat(filepath.Join(paths2.TempDir, "new"))
		Expect(os.IsNotExist(err)).To(BeTrue(), "new/ dir should not exist before transform")

		By("Run crane transform (offline)")
		runner2 := &CraneRunner{
			Bin:     config.CraneBin,
			WorkDir: paths2.TempDir,
		}
		log.Printf("Running crane transform with nested audit log path: %s\n", nestedAuditLog)
		Expect(runner2.Transform(TransformOptions{
			ExportDir:    testdataExportDir,
			TransformDir: paths2.TransformDir,
			AuditLogPath: nestedAuditLog,
		})).NotTo(HaveOccurred())
		log.Printf("Transform completed successfully\n")

		By("Verify audit log file does not exist at default path")
		defaultAuditLog2 := filepath.Join(paths2.TempDir, "audit", ".crane-audit.log")
		Expect(defaultAuditLog2).NotTo(BeAnExistingFile())

		By("Verify nested subdirectory was auto-created")
		info, err := os.Stat(filepath.Join(paths2.TempDir, "new", "nested", "subdir"))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.IsDir()).To(BeTrue(), "nested subdir should have been auto-created")

		By("Verify nested audit log file exists")
		Expect(nestedAuditLog).To(BeAnExistingFile())
		log.Printf("Audit log file exists at nested path: %s\n", nestedAuditLog)

		By("Read and parse audit log entries")
		entries2, err := utils.ReadAuditLogEntries(nestedAuditLog)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Read %d entries from audit log\n", len(entries2))

		By("Verify entries are not empty")
		Expect(entries2).NotTo(BeEmpty(), "audit log should contain entries")

	})
})
