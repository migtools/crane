package e2e

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PVC data integrity migration", func() {
	It("[MTC-197] Migrate a PVC with data and verify checksum integrity", Label("tier0"), func() {
		const testFileName = "testfile.txt"
		appName := "app-with-empty-pvc"
		namespace := appName
		scenario := NewMigrationScenario(
			"app-with-empty-pvc",
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		if scenario.KubectlSrcNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin test")
		}
		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin

		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
			"app_name":       appName,
			"add_data":       "true",
			"file_name":      testFileName,
			"file_size":      10,
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
			"app_name":       appName,
			"add_data":       "true",
		}

		By("Grant ns admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanup)

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup temp directory")
			if paths.TempDir != "" {
				log.Printf("Removing temp dir: %s\n", paths.TempDir)
				if err := os.RemoveAll(paths.TempDir); err != nil {
					log.Printf("cleanup: failed to remove temp dir %q: %v", paths.TempDir, err)
				}
			}
		})
		DeferCleanup(func() {
			By("Delete test namespace on source and target (wait for completion)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})

		By("SOURCE: List PVCs in the namespace")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).NotTo(BeEmpty(), "SOURCE: expected at least one PVC in namespace %q", srcApp.Namespace)
		log.Printf("SOURCE: Found %d PVCs in namespace %q", len(pvcs), srcApp.Namespace)
		for _, pvc := range pvcs {
			log.Printf("SOURCE: Found PVC %s in namespace %q\n", pvc.Name, pvc.Namespace)
		}

		By("SOURCE: Verify data file exists on PVC")
		fileName := testFileName
		output, err := kubectlSrcNonAdmin.Run("exec", appName, "-n", srcApp.Namespace, "--", "/bin/sh", "-c", fmt.Sprintf("ls -lh /data/%s", fileName))
		Expect(err).NotTo(HaveOccurred())
		log.Printf("SOURCE: File info: %s\n", output)

		By("SOURCE: Get file MD5 checksum")
		srcMD5Output, err := kubectlSrcNonAdmin.Run("exec", appName, "-n", srcApp.Namespace, "--", "/bin/sh", "-c", fmt.Sprintf("cat /data/%s.md5", fileName))
		Expect(err).NotTo(HaveOccurred())
		srcMD5 := strings.TrimSpace(srcMD5Output)
		Expect(srcMD5).NotTo(BeEmpty(), "expected MD5 checksum file to exist on source")
		log.Printf("Source: MD5 checksum: %s\n", srcMD5)

		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		runner := scenario.CraneNonAdmin
		runner.WorkDir = paths.TempDir
		Expect(RunCranePipelineWithChecks(runner, srcApp.Namespace, paths)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for source namespace %s\n", srcApp.Namespace)

		By("Transfer PVCs")
		tgtIP, err := GetClusterNodeIP(scenario.TgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		for _, pvc := range pvcs {
			pvcName := pvc.Name

			opts := TransferPVCOptions{
				SourceContext:   srcApp.Context,
				TargetContext:   tgtApp.Context,
				PVCName:         pvcName,
				PVCNamespaceMap: fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
				Endpoint:        "nginx-ingress",
				IngressClass:    "nginx",
				Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvcName, srcApp.Namespace, tgtIP),
			}
			log.Printf("Transferring PVC %s to namespace %s on target cluster", pvcName, tgtApp.Namespace)
			Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred())
			log.Printf("PVC transfer complete : %s -> namespace %s", pvcName, tgtApp.Namespace)
		}

		By("TARGET: List PVCs")
		tgtpvcs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtpvcs).NotTo(BeEmpty(), "expected at least one PVC in target namespace %q", tgtApp.Namespace)
		log.Printf("TARGET: Found %d PVCs in namespace %q", len(tgtpvcs), tgtApp.Namespace)

		// Verify each source PVC was transferred to target
		Expect(VerifyPVCsExistByName(pvcs, tgtpvcs)).NotTo(HaveOccurred())
		for _, pvc := range pvcs {
			log.Printf("TARGET: Verified PVC %s exists on target\n", pvc.Name)
		}

		By("TARGET: Apply rendered manifests")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", tgtApp.Namespace, paths.OutputDir)
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("TARGET: Validate application")
		log.Printf("TARGET: Validating app %s\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("TARGET: Validation completed for app %s\n", tgtApp.Name)

		By("TARGET: Verify data file exists after migration")
		tgtOutput, err := kubectlTgtNonAdmin.Run("exec", appName, "-n", tgtApp.Namespace, "--", "/bin/sh", "-c", fmt.Sprintf("ls -lh /data/%s", fileName))
		Expect(err).NotTo(HaveOccurred())
		log.Printf("TARGET: File info: %s\n", tgtOutput)

		By("TARGET: Verify MD5 checksum")
		tgtMD5Verify, err := kubectlTgtNonAdmin.Run("exec", appName, "-n", tgtApp.Namespace, "--", "/bin/sh", "-c", fmt.Sprintf("cd /data && md5sum -c %s.md5", fileName))
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtMD5Verify).To(ContainSubstring("OK"), "MD5 checksum verification should pass on target")
		log.Printf("TARGET: MD5 verification: %s\n", tgtMD5Verify)

		By("Compare source and target MD5 checksums")
		tgtMD5Output, err := kubectlTgtNonAdmin.Run("exec", appName, "-n", tgtApp.Namespace, "--", "/bin/sh", "-c", fmt.Sprintf("cat /data/%s.md5", fileName))
		Expect(err).NotTo(HaveOccurred())
		tgtMD5 := strings.TrimSpace(tgtMD5Output)
		Expect(tgtMD5).To(Equal(srcMD5), "MD5 checksum on target should match source")
		log.Printf("Source and target MD5 checksums match: %s\n", srcMD5)
	})
})
