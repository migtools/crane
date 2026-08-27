package e2e

import (
	"fmt"
	"log"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cross-cluster multi-PVC StorageClass conversion", func() {
	It("[MTA-903] Convert multiple PVCs in a namespace via sequential transfer-pvc invocations", Label("tier0", "pvc-transfer"), func() {
		const (
			appName            = "mysql"
			namespace          = "mta-903-mysql"
			fallbackDestSCName = "crane-dest-mta-903"
		)

		expectedPVCNames := []string{"mysql-data", "mysql-data1"}

		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin

		isOCP := scenario.KubectlSrc.IsOpenShift()
		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
			"has_scc":        isOCP,
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
			"has_scc":        isOCP,
		}

		By("Grant namespace admin permissions to the nonadmin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupActiveKubectlRunners(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			By("Delete test namespace on source and target")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(cleanup)

		paths, err := NewScenarioPaths("crane-mta-903-*")
		Expect(err).NotTo(HaveOccurred())
		runner.WorkDir = paths.TempDir

		var cleanupDestSC func() error
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
			By("Cleanup destination StorageClass if this test created one")
			if cleanupDestSC != nil {
				if err := cleanupDestSC(); err != nil {
					log.Printf("cleanup: %v", err)
				}
			}
		})

		By("Deploy and validate source MySQL app")
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

		By("Capture source data fingerprints for comparison")
		srcPodName, err := GetPodNameByLabel(kubectlSrcNonAdmin, srcApp.Namespace, "app="+appName)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() error {
			return WaitForMySQLSocket(kubectlSrcNonAdmin, srcApp.Namespace, srcPodName)
		}, "2m", "5s").Should(Succeed())
		sourceAuthorsCount, err := MySQLAuthorsCount(kubectlSrcNonAdmin, srcApp.Namespace, srcPodName)
		Expect(err).NotTo(HaveOccurred())
		sourceMD5Actual, sourceMD5Expected, err := MySQLTestDataMD5(kubectlSrcNonAdmin, srcApp.Namespace, srcPodName)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceMD5Actual).To(Equal(sourceMD5Expected), "source test-data checksum should match its md5 file")

		By("List MySQL PVCs and confirm the expected two claims exist")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).To(HaveLen(len(expectedPVCNames)), "expected exactly %d PVCs in namespace %q", len(expectedPVCNames), srcApp.Namespace)
		expectPVCNames(pvcs, expectedPVCNames)

		By("Resolve the source StorageClass and choose a distinct destination class on target")
		var sourceSC string
		for i, pvcName := range expectedPVCNames {
			srcPVC, err := GetPVC(srcApp.Context, srcApp.Namespace, pvcName)
			Expect(err).NotTo(HaveOccurred())
			resolvedSC, err := ResolvePVCStorageClass(scenario.KubectlSrc.Context, *srcPVC)
			Expect(err).NotTo(HaveOccurred())
			Expect(resolvedSC).NotTo(BeEmpty(), "source PVC %s/%s must have a StorageClass", srcApp.Namespace, pvcName)
			if i == 0 {
				sourceSC = resolvedSC
				continue
			}
			Expect(resolvedSC).To(Equal(sourceSC),
				"expected all source PVCs to use the same StorageClass before conversion")
		}

		var destSCName string
		if scenario.KubectlTgt.IsOpenShift() {
			destSCName, cleanupDestSC, err = PrepareDestinationStorageClass(scenario.KubectlTgt.Context, sourceSC, fallbackDestSCName)
			Expect(err).NotTo(HaveOccurred())
			Expect(destSCName).NotTo(Equal(sourceSC))
		} else {
			targetDefaultSC, err := DefaultStorageClassName(scenario.KubectlTgt.Context)
			Expect(err).NotTo(HaveOccurred())
			Expect(targetDefaultSC).NotTo(BeEmpty(), "target cluster must expose a default StorageClass for fallback cloning")

			created, err := CloneStorageClass(scenario.KubectlTgt.Context, targetDefaultSC, fallbackDestSCName)
			Expect(err).NotTo(HaveOccurred())
			destSCName = fallbackDestSCName
			if created {
				cleanupDestSC = func() error {
					return DeleteStorageClass(scenario.KubectlTgt.Context, fallbackDestSCName)
				}
			} else {
				cleanupDestSC = func() error { return nil }
			}
		}

		By("Quiesce the source MySQL deployment before export and transfer")
		Expect(kubectlSrcNonAdmin.ScaleDeploymentIfPresent(srcApp.Namespace, srcApp.Name, 0)).NotTo(HaveOccurred())
		Eventually(func() (string, error) {
			out, err := kubectlSrcNonAdmin.Run(
				"get", "pods",
				"--namespace", namespace,
				"-l", "app="+appName,
				"-o", "name",
			)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(out), nil
		}, "90s", "3s").Should(BeEmpty())

		By("Run crane export/transform/apply pipeline")
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Transfer each MySQL PVC sequentially onto the destination StorageClass")
		tgtIP, err := GetClusterNodeIP(scenario.KubectlTgt.Context)
		Expect(err).NotTo(HaveOccurred())
		for _, pvcName := range expectedPVCNames {
			opts := TransferPVCOptions{
				SourceContext:    srcApp.Context,
				TargetContext:    tgtApp.Context,
				PVCName:          pvcName,
				PVCNamespaceMap:  fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
				DestStorageClass: destSCName,
				Subdomain:        fmt.Sprintf("%s.%s.%s.nip.io", pvcName, srcApp.Namespace, tgtIP),
			}
			log.Printf("Transferring PVC %s to namespace %s on target cluster with StorageClass=%s", pvcName, tgtApp.Namespace, destSCName)
			Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred())
			AssertNoTransferPVCLeftovers(kubectlSrcNonAdmin, []string{srcApp.Namespace}, pvcName)
			AssertNoTransferPVCLeftovers(kubectlTgtNonAdmin, []string{tgtApp.Namespace}, pvcName)
		}

		By("Verify target PVCs exist and use the converted StorageClass")
		tgtPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtPVCs).To(HaveLen(len(expectedPVCNames)), "expected exactly %d PVCs in target namespace %q", len(expectedPVCNames), tgtApp.Namespace)
		Expect(VerifyPVCsExistByName(pvcs, tgtPVCs)).NotTo(HaveOccurred())
		for _, pvcName := range expectedPVCNames {
			destPVC, err := GetPVC(tgtApp.Context, tgtApp.Namespace, pvcName)
			Expect(err).NotTo(HaveOccurred())
			Expect(PVCStorageClassName(*destPVC)).To(Equal(destSCName),
				"destination PVC %s/%s StorageClass should be %s", tgtApp.Namespace, pvcName, destSCName)
			Expect(destPVC.Status.Phase).To(Equal(corev1.ClaimBound),
				"destination PVC %s/%s should be Bound after transfer", tgtApp.Namespace, pvcName)
			Expect(VerifyPVCSchedulable(scenario.KubectlTgt.Context, tgtApp.Namespace, pvcName)).NotTo(HaveOccurred())
		}

		By("Verify each transferred PVC contains data before the workload starts")
		Expect(VerifyPVCHasData(kubectlTgtNonAdmin, tgtApp.Namespace, "mysql-data", "/var/lib/mysql")).NotTo(HaveOccurred())
		Expect(VerifyPVCHasData(kubectlTgtNonAdmin, tgtApp.Namespace, "mysql-data1", "/test-data")).NotTo(HaveOccurred())

		By("Apply rendered manifests to target")
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale the target MySQL deployment and validate the app")
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
		Eventually(tgtApp.Validate, "5m", "10s").Should(Succeed())

		var tgtPodName string
		Eventually(func() error {
			podName, err := GetPodNameByLabel(kubectlTgtNonAdmin, tgtApp.Namespace, "app="+appName)
			if err != nil {
				return err
			}
			tgtPodName = podName
			out, err := kubectlTgtNonAdmin.Run(
				"get", "pod", tgtPodName,
				"-n", tgtApp.Namespace,
				"-o", "jsonpath={.status.containerStatuses[0].ready}",
			)
			if err != nil {
				return err
			}
			if strings.TrimSpace(out) != "true" {
				return fmt.Errorf("pod %s is not ready yet", tgtPodName)
			}
			return nil
		}, "2m", "10s").Should(Succeed())
		Eventually(func() error {
			return WaitForMySQLSocket(kubectlTgtNonAdmin, tgtApp.Namespace, tgtPodName)
		}, "2m", "5s").Should(Succeed())

		By("Verify both migrated MySQL volumes contain the correct, uncontaminated data")
		targetAuthorsCount, err := MySQLAuthorsCount(kubectlTgtNonAdmin, tgtApp.Namespace, tgtPodName)
		Expect(err).NotTo(HaveOccurred())
		targetMD5Actual, targetMD5Expected, err := MySQLTestDataMD5(kubectlTgtNonAdmin, tgtApp.Namespace, tgtPodName)
		Expect(err).NotTo(HaveOccurred())
		Expect(targetMD5Actual).To(Equal(targetMD5Expected), "target test-data checksum should match its md5 file")
		Expect(targetAuthorsCount).To(Equal(sourceAuthorsCount), "authors count should match between source and target")
		Expect(targetMD5Actual).To(Equal(sourceMD5Actual), "test-data md5 should match between source and target")

		By("Confirm no leftover transfer-pvc resources remain for either PVC")
		for _, pvcName := range expectedPVCNames {
			AssertNoTransferPVCLeftovers(kubectlSrcNonAdmin, []string{srcApp.Namespace}, pvcName)
			AssertNoTransferPVCLeftovers(kubectlTgtNonAdmin, []string{tgtApp.Namespace}, pvcName)
		}
	})
})

func expectPVCNames(pvcs []corev1.PersistentVolumeClaim, expectedNames []string) {
	GinkgoHelper()

	actualNames := make(map[string]bool, len(pvcs))
	for _, pvc := range pvcs {
		actualNames[pvc.Name] = true
	}

	for _, expectedName := range expectedNames {
		Expect(actualNames).To(HaveKey(expectedName),
			"expected PVC %q to exist in the MySQL app PVC set", expectedName)
	}
}
