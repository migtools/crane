package e2e

import (
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Same-cluster PVC rename + StorageClass conversion", func() {
	It("[MTA-901] Rename PVC and convert StorageClass with pvc-rename-map transform, same cluster", Label("tier0", "pvc-transfer"), func() {
		const (
			appName            = "mongodb"
			srcPVCName         = "mongodb-data"
			dstPVCName         = "mongodb-data-new"
			fallbackDestSCName = "crane-dest-mta-901"
		)
		srcNamespace := "mta-901-src"
		tgtNamespace := "mta-901-tgt"

		scenario := NewMigrationScenario(
			appName,
			srcNamespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.SourceContext,
		)
		scenario.TgtAppNonAdmin.Namespace = tgtNamespace
		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin

		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		By("Grant namespace admin permissions to the nonadmin user on both namespaces")
		kubectlSrc, cleanupSrc, err := SetupActiveNamespaceAdmin(scenario.KubectlSrc, scenario.KubectlSrcNonAdmin.Context, srcNamespace)
		Expect(err).NotTo(HaveOccurred())
		kubectlTgt, cleanupTgt, err := SetupActiveNamespaceAdmin(scenario.KubectlTgt, scenario.KubectlTgtNonAdmin.Context, tgtNamespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanupTgt)
		DeferCleanup(cleanupSrc)

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{
			ExportDir:    paths.ExportDir,
			TransformDir: paths.TransformDir,
			OptionalFlags: fmt.Sprintf(`{"pvc-rename-map":"%s:%s"}`, srcPVCName, dstPVCName),
		}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}

		var destSCName string
		var cleanupDestSC func() error
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
			By("Delete source and target namespaces")
			for _, ns := range []string{srcNamespace, tgtNamespace} {
				if _, err := scenario.KubectlSrc.Run("delete", "namespace", ns, "--ignore-not-found=true", "--wait=true", "--timeout=60s"); err != nil {
					log.Printf("cleanup: %v", err)
				}
			}
			By("Cleanup destination StorageClass if this test created one")
			if cleanupDestSC != nil {
				if err := cleanupDestSC(); err != nil {
					log.Printf("cleanup: %v", err)
				}
			}
		})

		By("Deploy and validate source MongoDB")
		log.Printf("Preparing source app %s in namespace %s", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

		By("Resolve source StorageClass and choose a distinct destination class")
		srcPVC, err := GetPVC(srcApp.Context, srcNamespace, srcPVCName)
		Expect(err).NotTo(HaveOccurred())
		srcSC, err := ResolvePVCStorageClass(scenario.SrcApp.Context, *srcPVC)
		Expect(err).NotTo(HaveOccurred())
		Expect(srcSC).NotTo(BeEmpty(), "source PVC %s/%s must have a StorageClass", srcNamespace, srcPVCName)
		log.Printf("Source PVC %s/%s StorageClass=%s", srcNamespace, srcPVCName, srcSC)

		destSCName, cleanupDestSC, err = PrepareDestinationStorageClass(scenario.SrcApp.Context, srcSC, fallbackDestSCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(destSCName).NotTo(Equal(srcSC))
		log.Printf("Using destination StorageClass=%s", destSCName)

		By("Scale down source MongoDB so the RWO PVC is unmounted")
		Expect(kubectlSrc.ScaleDeploymentIfPresent(srcNamespace, appName, 0)).NotTo(HaveOccurred())
		_, err = kubectlSrc.Run("wait", "pod", "-n", srcNamespace, "-l", "name="+appName, "--for=delete", "--timeout=60s")
		Expect(err).NotTo(HaveOccurred())
		WaitForSourceQuiesce(kubectlSrc, srcNamespace, "name="+appName, appName)

		By("Transfer PVC with rename onto the destination StorageClass")
		nodeIP, err := GetClusterNodeIP(scenario.SrcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		opts := TransferPVCOptions{
			SourceContext:    srcApp.Context,
			TargetContext:    tgtApp.Context,
			PVCName:          fmt.Sprintf("%s:%s", srcPVCName, dstPVCName),
			PVCNamespaceMap:  fmt.Sprintf("%s:%s", srcNamespace, tgtNamespace),
			DestStorageClass: destSCName,
			Subdomain:        fmt.Sprintf("%s.nip.io", nodeIP),
		}
		log.Printf("Transferring PVC %s -> %s (%s -> %s) with dest StorageClass %s",
			srcPVCName, dstPVCName, srcNamespace, tgtNamespace, destSCName)
		Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred())

		By("Wait for transfer-pvc helpers to finish deleting")
		AssertNoTransferPVCLeftovers(kubectlSrc, []string{srcNamespace}, srcPVCName)
		AssertNoTransferPVCLeftovers(kubectlTgt, []string{tgtNamespace}, srcPVCName)

		By("Assert destination PVC exists with the new name and converted StorageClass")
		destPVC, err := GetPVC(tgtApp.Context, tgtNamespace, dstPVCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(PVCStorageClassName(*destPVC)).To(Equal(destSCName),
			"destination PVC StorageClass should be %s", destSCName)
		log.Printf("Destination PVC %s/%s created with StorageClass=%s", tgtNamespace, dstPVCName, destSCName)

		By("Run crane export/transform/apply pipeline with pvc-rename-map")
		runner.WorkDir = paths.TempDir
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verify the pvc-rename-map transform rewrote claimName in the rendered output")
		VerifyPVCRenameInOutput(paths.OutputDir, srcPVCName, dstPVCName)

		By("Apply remapped manifests to destination namespace")
		Expect(ApplyOutputToTargetWithNamespaceRemapNonAdmin(kubectlTgt, srcNamespace, tgtNamespace, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale destination MongoDB and validate data")
		Expect(kubectlTgt.ScaleDeployment(tgtNamespace, appName, 1)).NotTo(HaveOccurred())
		Eventually(tgtApp.Validate, "5m", "10s").Should(Succeed())

		By("Assert destination PVC is Bound after the workload starts")
		destPVC, err = GetPVC(tgtApp.Context, tgtNamespace, dstPVCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(destPVC.Status.Phase).To(Equal(corev1.ClaimBound))

		By("Confirm no leftover transfer-pvc resources")
		AssertNoTransferPVCLeftovers(kubectlSrc, []string{srcNamespace}, srcPVCName)
		AssertNoTransferPVCLeftovers(kubectlTgt, []string{tgtNamespace}, srcPVCName)
	})
})

