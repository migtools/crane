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

var _ = Describe("Non-admin MySQL StorageClass conversion", func() {
	It("[MTA-904] Converts MySQL PVCs to a different StorageClass using namespace-admin RBAC", Label("tier0", "pvc-transfer"), func() {
		const (
			appName            = "mysql"
			namespace          = "mta-904-mysql"
			fallbackDestSCName = "crane-dest-mta-904"
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

		srcIsOCP := scenario.KubectlSrc.IsOpenShift()
		tgtIsOCP := scenario.KubectlTgt.IsOpenShift()
		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
			"has_scc":        srcIsOCP,
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
			"has_scc":        tgtIsOCP,
		}

		By("Grant namespace-admin permissions to the non-admin users")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanupRBAC, err := SetupActiveKubectlRunners(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			By("Delete source and target namespaces")
			for _, kubectl := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := kubectl.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=120s"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, kubectl.Context, err)
				}
			}
		})
		DeferCleanup(cleanupRBAC)

		paths, err := NewScenarioPaths("crane-mta-904-*")
		Expect(err).NotTo(HaveOccurred())
		runner.WorkDir = paths.TempDir

		var cleanupDestSC func() error
		DeferCleanup(func() {
			By("Clean up source and target applications, temporary files, and destination StorageClass")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
			if cleanupDestSC != nil {
				if err := cleanupDestSC(); err != nil {
					log.Printf("cleanup: failed to remove destination StorageClass: %v", err)
				}
			}
		})

		By("Deploy and validate source MySQL as a non-admin user")
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

		By("Capture source MySQL data fingerprints")
		sourcePodName, err := GetPodNameByLabel(kubectlSrcNonAdmin, srcApp.Namespace, "app="+appName)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() error {
			return WaitForMySQLSocket(kubectlSrcNonAdmin, srcApp.Namespace, sourcePodName)
		}, "2m", "5s").Should(Succeed())
		sourceAuthorsCount, err := MySQLAuthorsCount(kubectlSrcNonAdmin, srcApp.Namespace, sourcePodName)
		Expect(err).NotTo(HaveOccurred())
		sourceMD5Actual, sourceMD5Expected, err := MySQLTestDataMD5(kubectlSrcNonAdmin, srcApp.Namespace, sourcePodName)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceMD5Actual).To(Equal(sourceMD5Expected), "source test-data checksum should match its md5 file")

		By("List source MySQL PVCs")
		sourcePVCs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourcePVCs).To(HaveLen(len(expectedPVCNames)))
		Expect(VerifyPVCNames(sourcePVCs, expectedPVCNames)).NotTo(HaveOccurred())

		By("Resolve source StorageClass and prepare a different target StorageClass")
		var sourceSC string
		for i, pvcName := range expectedPVCNames {
			sourcePVC, err := GetPVC(srcApp.Context, srcApp.Namespace, pvcName)
			Expect(err).NotTo(HaveOccurred())
			resolvedSC, err := ResolvePVCStorageClass(scenario.KubectlSrc.Context, *sourcePVC)
			Expect(err).NotTo(HaveOccurred())
			Expect(resolvedSC).NotTo(BeEmpty())
			if i == 0 {
				sourceSC = resolvedSC
			} else {
				Expect(resolvedSC).To(Equal(sourceSC), "all MySQL PVCs should use the same source StorageClass")
			}
		}

		var destinationSC string
		if tgtIsOCP {
			destinationSC, cleanupDestSC, err = PrepareDestinationStorageClass(scenario.KubectlTgt.Context, sourceSC, fallbackDestSCName)
			Expect(err).NotTo(HaveOccurred())
		} else {
			targetDefaultSC, err := DefaultStorageClassName(scenario.KubectlTgt.Context)
			Expect(err).NotTo(HaveOccurred())
			Expect(targetDefaultSC).NotTo(BeEmpty(), "target cluster must expose a default StorageClass for fallback cloning")
			created, err := CloneStorageClass(scenario.KubectlTgt.Context, targetDefaultSC, fallbackDestSCName)
			Expect(err).NotTo(HaveOccurred())
			destinationSC = fallbackDestSCName
			if created {
				cleanupDestSC = func() error {
					return DeleteStorageClass(scenario.KubectlTgt.Context, fallbackDestSCName)
				}
			}
		}
		Expect(destinationSC).NotTo(BeEmpty())
		Expect(destinationSC).NotTo(Equal(sourceSC))

		By("Quiesce source MySQL before export and transfer")
		Expect(kubectlSrcNonAdmin.ScaleDeploymentIfPresent(srcApp.Namespace, srcApp.Name, 0)).NotTo(HaveOccurred())
		Eventually(func() (string, error) {
			out, err := kubectlSrcNonAdmin.Run("get", "pods", "-n", srcApp.Namespace, "-l", "app="+appName, "-o", "name")
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(StripKubectlWarnings(out)), nil
		}, "2m", "5s").Should(BeEmpty())

		By("Run export, transform, and apply rendering through the non-admin Crane runner")
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Transfer both MySQL PVCs sequentially through the non-admin Crane runner")
		targetNodeIP, err := GetClusterNodeIP(scenario.KubectlTgt.Context)
		Expect(err).NotTo(HaveOccurred())
		for _, pvcName := range expectedPVCNames {
			Expect(runner.TransferPVC(TransferPVCOptions{
				SourceContext:    srcApp.Context,
				TargetContext:    tgtApp.Context,
				PVCName:          pvcName,
				PVCNamespaceMap:  fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
				DestStorageClass: destinationSC,
				Subdomain:        fmt.Sprintf("%s.%s.%s.nip.io", pvcName, srcApp.Namespace, targetNodeIP),
			})).NotTo(HaveOccurred())
			AssertNoTransferPVCLeftovers(kubectlSrcNonAdmin, []string{srcApp.Namespace}, pvcName)
			AssertNoTransferPVCLeftovers(kubectlTgtNonAdmin, []string{tgtApp.Namespace}, pvcName)
		}

		By("Verify target PVCs are Bound and use the destination StorageClass")
		targetPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(targetPVCs).To(HaveLen(len(expectedPVCNames)))
		Expect(VerifyPVCNames(targetPVCs, expectedPVCNames)).NotTo(HaveOccurred())
		for _, pvcName := range expectedPVCNames {
			destinationPVC, err := GetPVC(tgtApp.Context, tgtApp.Namespace, pvcName)
			Expect(err).NotTo(HaveOccurred())
			Expect(PVCStorageClassName(*destinationPVC)).To(Equal(destinationSC))
			Expect(destinationPVC.Status.Phase).To(Equal(corev1.ClaimBound))
		}

		By("Verify both transferred PVCs contain data before starting MySQL")
		Expect(VerifyPVCHasData(kubectlTgtNonAdmin, tgtApp.Namespace, "mysql-data", "/var/lib/mysql")).NotTo(HaveOccurred())
		Expect(VerifyPVCHasData(kubectlTgtNonAdmin, tgtApp.Namespace, "mysql-data1", "/test-data")).NotTo(HaveOccurred())

		By("Apply rendered manifests to the target namespace as a non-admin user")
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Start target MySQL and verify data integrity")
		Expect(kubectlTgtNonAdmin.ScaleDeployment(tgtApp.Namespace, appName, 1)).NotTo(HaveOccurred())
		Eventually(tgtApp.Validate, "5m", "10s").Should(Succeed())
		targetPodName, err := GetPodNameByLabel(kubectlTgtNonAdmin, tgtApp.Namespace, "app="+appName)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() error {
			return WaitForMySQLSocket(kubectlTgtNonAdmin, tgtApp.Namespace, targetPodName)
		}, "2m", "5s").Should(Succeed())
		targetAuthorsCount, err := MySQLAuthorsCount(kubectlTgtNonAdmin, tgtApp.Namespace, targetPodName)
		Expect(err).NotTo(HaveOccurred())
		targetMD5Actual, targetMD5Expected, err := MySQLTestDataMD5(kubectlTgtNonAdmin, tgtApp.Namespace, targetPodName)
		Expect(err).NotTo(HaveOccurred())
		Expect(targetMD5Actual).To(Equal(targetMD5Expected), "target test-data checksum should match its md5 file")
		Expect(targetAuthorsCount).To(Equal(sourceAuthorsCount), "authors count should match between source and target")
		Expect(targetMD5Actual).To(Equal(sourceMD5Actual), "test-data md5 should match between source and target")

		By("Confirm no transfer-pvc helper resources remain")
		for _, pvcName := range expectedPVCNames {
			AssertNoTransferPVCLeftovers(kubectlSrcNonAdmin, []string{srcApp.Namespace}, pvcName)
			AssertNoTransferPVCLeftovers(kubectlTgtNonAdmin, []string{tgtApp.Namespace}, pvcName)
		}
	})
})
