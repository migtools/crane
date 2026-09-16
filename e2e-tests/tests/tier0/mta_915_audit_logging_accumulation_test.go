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

var _ = Describe("Audit logging multi-command accumulation", func() {
	It("[MTA-915] transform + apply accumulate entries in default audit log", Label("tier0"), func() {
		paths, err := NewScenarioPaths("crane-audit-*")
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

		transformOpts := TransformOptions{
			ExportDir:    testdataExportDir,
			TransformDir: paths.TransformDir,
		}
		applyOpts := ApplyOptions{
			TransformDir: paths.TransformDir,
			OutputDir:    paths.OutputDir,
		}

		By("Run crane transform (offline)")
		log.Printf("Running crane transform from %s to %s\n", testdataExportDir, paths.TransformDir)
		Expect(runner.Transform(transformOpts)).NotTo(HaveOccurred())
		log.Printf("Transform completed successfully\n")

		By("Run crane apply (offline)")
		log.Printf("Running crane apply from %s to %s\n", paths.TransformDir, paths.OutputDir)
		Expect(runner.Apply(applyOpts)).NotTo(HaveOccurred())
		log.Printf("Apply completed successfully\n")

		defaultAuditLog := filepath.Join(paths.TempDir, "audit", ".crane-audit.log")

		By("Verify audit directory exists")
		auditDir := filepath.Join(paths.TempDir, "audit")
		info, err := os.Stat(auditDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.IsDir()).To(BeTrue(), "audit directory should exist")
		log.Printf("Audit directory exists: %s\n", auditDir)

		By("Verify audit log file exists with 0600 permissions")
		fileInfo, err := os.Stat(defaultAuditLog)
		Expect(err).NotTo(HaveOccurred())
		Expect(fileInfo.Mode().Perm()).To(Equal(os.FileMode(0o600)))
		log.Printf("Audit log file exists with 0600 permissions: %s\n", defaultAuditLog)

		By("Read and parse audit log entries")
		entries, err := utils.ReadAuditLogEntries(defaultAuditLog)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Read %d entries from audit log\n", len(entries))

		By("Verify entries are not empty")
		Expect(entries).NotTo(BeEmpty(), "audit log should contain entries")

		By("Verify transform entries precede apply entries")
		transformIndex := -1
		applyIndex := -1
		for i, entry := range entries {
			if cmd, ok := entry["cmd"].(string); ok {
				if cmd == "transform" && transformIndex == -1 {
					transformIndex = i
				}
				if cmd == "apply" && applyIndex == -1 {
					applyIndex = i
				}
			}
		}
		Expect(transformIndex).NotTo(Equal(-1), "audit log should contain transform entries")
		Expect(applyIndex).NotTo(Equal(-1), "audit log should contain apply entries")
		Expect(transformIndex).To(BeNumerically("<", applyIndex), "transform entries should appear before apply entries")
		log.Printf("Transform entries at index %d, apply entries at index %d\n", transformIndex, applyIndex)
	})
})
