package e2e

import (
	"log"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	"github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Audit logging multi-command accumulation", func() {
	It("[MTA-915] transform + apply accumulate entries in default audit log", Label("tier0"), func() {
		paths, err := framework.NewScenarioPaths("crane-audit-*")
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Created temp directory: %s\n", paths.TempDir)

		testdataExportDir, err := utils.TestdataFilePath("audit-log-export")
		Expect(err).NotTo(HaveOccurred())

		runner := &framework.CraneRunner{
			Bin:     config.CraneBin,
			WorkDir: paths.TempDir,
		}

		transformOpts := framework.TransformOptions{
			ExportDir:    testdataExportDir,
			TransformDir: paths.TransformDir,
		}
		applyOpts := framework.ApplyOptions{
			TransformDir: paths.TransformDir,
			OutputDir:    paths.OutputDir,
		}

		DeferCleanup(func() {
			By("Cleanup temp directory")
			if err := os.RemoveAll(paths.TempDir); err != nil {
				log.Printf("cleanup: failed to remove temp dir: %v", err)
			}
		})

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

		By("Verify transform entries exist")
		hasTransformEntry := false
		for _, entry := range entries {
			if cmd, ok := entry["cmd"].(string); ok && cmd == "transform" {
				hasTransformEntry = true
				break
			}
		}
		Expect(hasTransformEntry).To(BeTrue(), "audit log should contain transform entries")
		log.Printf("Found transform entries\n")

		By("Verify apply entries exist")
		hasApplyEntry := false
		for _, entry := range entries {
			if cmd, ok := entry["cmd"].(string); ok && cmd == "apply" {
				hasApplyEntry = true
				break
			}
		}
		Expect(hasApplyEntry).To(BeTrue(), "audit log should contain apply entries")
		log.Printf("Found apply entries\n")

		By("Verify transform entries appear before apply entries")
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
		Expect(transformIndex).NotTo(Equal(-1), "transform entries should exist")
		Expect(applyIndex).NotTo(Equal(-1), "apply entries should exist")
		Expect(transformIndex < applyIndex).To(BeTrue(), "transform entries should appear before apply entries")
		log.Printf("Transform entries appear at index %d, apply entries at index %d\n", transformIndex, applyIndex)

		By("Verify JSON validity and required fields (covered by ReadAuditLogEntries)")
		for i, entry := range entries {
			Expect(entry).To(HaveKey("cmd"))
			Expect(entry).To(HaveKey("level"))
			Expect(entry).To(HaveKey("msg"))
			Expect(entry).To(HaveKey("time"))
			log.Printf("Entry %d: cmd=%s, level=%s\n", i, entry["cmd"], entry["level"])
		}
	})
})
