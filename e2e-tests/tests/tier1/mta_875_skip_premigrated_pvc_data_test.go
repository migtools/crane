package e2e

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Skip PV migration when PV data was already migrated ahead of time", func() {
	It("[MTA-875] Skip PV migration when PV data was already migrated ahead of time", Label("tier1", "pvc-transfer"), func() {
		const testFileName = "testfile.txt"
		appName := "app-with-empty-pvc"
		namespace := "mta-875-skip-pv"
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		srcApp := scenario.SrcApp
		tgtApp := scenario.TgtApp
		kubectlSrc := scenario.KubectlSrc
		kubectlTgt := scenario.KubectlTgt

		srcApp.ExtraVars = map[string]any{
			"app_name":  appName,
			"add_data":  "true",
			"file_name": testFileName,
			"file_size": 10,
		}
		tgtApp.ExtraVars = map[string]any{
			"app_name": appName,
			"add_data": "true",
		}

		By("Deploy a source app with a PVC and seed it with known data")
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		By("Get file MD5 checksum")
		srcMD5Output, err := kubectlSrc.Run("exec", appName, "-n", srcApp.Namespace, "--", "/bin/sh", "-c", fmt.Sprintf("cat /data/%s.md5", testFileName))
		Expect(err).NotTo(HaveOccurred())
		srcMD5 := strings.TrimSpace(srcMD5Output)
		Expect(srcMD5).NotTo(BeEmpty(), "expected MD5 checksum file to exist on source")
		log.Printf("MD5 checksum: %s\n", srcMD5)

		By("List PVCs in the namespace")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).To(HaveLen(1), "expected exactly one PVC in namespace %q", srcApp.Namespace)
		pvcName := pvcs[0].Name
		log.Printf("Found PVC %s in namespace %q\n", pvcName, srcApp.Namespace)

		By("Create target namespace")
		Expect(kubectlTgt.CreateNamespace(tgtApp.Namespace)).NotTo(HaveOccurred())

		By("Manually run crane transfer-pvc to migrate the PVC data to the target cluster ahead of the main app migration")
		runner := scenario.Crane
		runner.WorkDir = paths.TempDir
		tgtIP, err := GetClusterNodeIP(tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		transferOpts := TransferPVCOptions{
			SourceContext:   srcApp.Context,
			TargetContext:   tgtApp.Context,
			PVCName:         pvcName,
			PVCNamespaceMap: fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
			Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvcName, srcApp.Namespace, tgtIP),
		}
		Expect(runner.TransferPVC(transferOpts)).NotTo(HaveOccurred())

		By("Wait for transfer-pvc's ephemeral rsync pods to be fully removed")
		Eventually(func() (string, error) {
			return kubectlSrc.Run("get", "pods", "-n", srcApp.Namespace, "-o", "name")
		}, "60s", "2s").ShouldNot(ContainSubstring("rsync"))
		Eventually(func() (string, error) {
			return kubectlTgt.Run("get", "pods", "-n", tgtApp.Namespace, "-o", "name")
		}, "60s", "2s").ShouldNot(ContainSubstring("rsync"))

		By("Verify the destination PVC exists")
		tgtPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtPVCs).To(HaveLen(1), "expected the pre-migrated PVC to exist on target")

		By("Run crane export/transform/apply for the app's namespace (PVCs whited out as usual)")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verify the rendered manifests contain no PVC/PV: the already-migrated PVC is not re-transferred or re-created")
		outputFiles, err := utils.ListFilesRecursivelyAsList(paths.OutputDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(outputFiles).NotTo(BeEmpty())
		for _, f := range outputFiles {
			Expect(f).NotTo(ContainSubstring("PersistentVolume"), "output should not contain a PVC/PV manifest file: %s", f)
			content, err := os.ReadFile(filepath.Join(paths.OutputDir, f))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).NotTo(ContainSubstring("PersistentVolume"), "output file %s should not reference a PVC/PV", f)
		}

		By("Deploy/scale up the app on the target cluster, reusing the pre-migrated PVC")
		Expect(ApplyOutputToTarget(kubectlTgt, tgtApp.Namespace, paths.OutputDir)).NotTo(HaveOccurred())
		Eventually(tgtApp.Validate, "5m", "10s").Should(Succeed())

		By("Verify the app reads the pre-migrated data with no data loss")
		tgtMD5Output, err := kubectlTgt.Run("exec", appName, "-n", tgtApp.Namespace, "--", "/bin/sh", "-c", fmt.Sprintf("cat /data/%s.md5", testFileName))
		Expect(err).NotTo(HaveOccurred())
		tgtMD5 := strings.TrimSpace(tgtMD5Output)
		Expect(tgtMD5).To(Equal(srcMD5), "MD5 checksum on target should match source")
		log.Printf("Source and target MD5 checksums match: %s\n", srcMD5)
	})
})
