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
			"file_name":      "testfile.txt",
			"file_size":      10,
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
			"app_name":       appName,
			"add_data":       "false",
		}

		By("Grant ns admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanup)

		By("Deploy and validate source app")
		log.Printf("Deploying source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(srcApp.Deploy()).NotTo(HaveOccurred())
		Expect(srcApp.Validate()).NotTo(HaveOccurred())
		log.Printf("Source app %s deployed and validated successfully\n", srcApp.Name)

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
			By("Delete test namespace on source and target (best effort)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})

		By("List PVCs in the source namespace")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).NotTo(BeEmpty(), "expected at least one PVC in source namespace %q", srcApp.Namespace)
		log.Printf("Found %d PVCs in source namespace %q", len(pvcs), srcApp.Namespace)
		for _, pvc := range pvcs {
			log.Printf("Found PVC %s in source namespace %q\n", pvc.Name, pvc.Namespace)
		}

		By("Verify data file exists on source cluster")
		fileName := srcApp.ExtraVars["file_name"].(string)
		output, err := kubectlSrcNonAdmin.Run("exec", appName, "-n", srcApp.Namespace, "--", "/bin/sh", "-c", fmt.Sprintf("ls -lh /data/%s", fileName))
		Expect(err).NotTo(HaveOccurred())
		log.Printf("File info on source: %s\n", output)

		By("Get file MD5 checksum on source cluster")
		srcMD5Output, err := kubectlSrcNonAdmin.Run("exec", appName, "-n", srcApp.Namespace, "--", "/bin/sh", "-c", fmt.Sprintf("cat /data/%s.md5", fileName))
		Expect(err).NotTo(HaveOccurred())
		srcMD5 := strings.TrimSpace(srcMD5Output)
		Expect(srcMD5).NotTo(BeEmpty(), "expected MD5 checksum file to exist on source")
		log.Printf("Source MD5 checksum: %s\n", srcMD5)

		By("Quiesce source app")
		Expect(kubectlSrcNonAdmin.ScaleDeploymentIfPresent(srcApp.Namespace, srcApp.Name, 0)).NotTo(HaveOccurred())

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

		By("List PVCs on target cluster")
		tgtpvcs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtpvcs).NotTo(BeEmpty(), "expected at least one PVC in target namespace %q", tgtApp.Namespace)
		log.Printf("Found %d PVCs in target namespace %q", len(tgtpvcs), tgtApp.Namespace)

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", tgtApp.Namespace, paths.OutputDir)
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Validate target application")
		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)

		By("Verify data file exists on target cluster after migration")
		tgtOutput, err := kubectlTgtNonAdmin.Run("exec", appName, "-n", tgtApp.Namespace, "--", "/bin/sh", "-c", fmt.Sprintf("ls -lh /data/%s", fileName))
		Expect(err).NotTo(HaveOccurred())
		log.Printf("File info on target: %s\n", tgtOutput)

		By("Verify MD5 checksum on target cluster")
		tgtMD5Verify, err := kubectlTgtNonAdmin.Run("exec", appName, "-n", tgtApp.Namespace, "--", "/bin/sh", "-c", fmt.Sprintf("cd /data && md5sum -c %s.md5", fileName))
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtMD5Verify).To(ContainSubstring("OK"), "MD5 checksum verification should pass on target")
		log.Printf("MD5 verification on target: %s\n", tgtMD5Verify)

		By("Compare source and target MD5 checksums")
		tgtMD5Output, err := kubectlTgtNonAdmin.Run("exec", appName, "-n", tgtApp.Namespace, "--", "/bin/sh", "-c", fmt.Sprintf("cat /data/%s.md5", fileName))
		Expect(err).NotTo(HaveOccurred())
		tgtMD5 := strings.TrimSpace(tgtMD5Output)
		Expect(tgtMD5).To(Equal(srcMD5), "MD5 checksum on target should match source")
		log.Printf("Source and target MD5 checksums match: %s\n", srcMD5)
	})
})
