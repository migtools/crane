package e2e

import (
	"fmt"
	"log"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Same-cluster transfer-pvc", func() {
	It("[MTA-905] transfers MongoDB data between namespaces on the same cluster", Label("tier0", "pvc-transfer"), func() {
		const (
			appName      = "mongodb"
			srcNamespace = "mta-905-src"
			tgtNamespace = "mta-905-tgt"
			pvcName      = "mongodb-data"
		)

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
		paths, err := NewScenarioPaths("crane-mta-905-*")
		Expect(err).NotTo(HaveOccurred())
		runner.WorkDir = paths.TempDir

		srcApp.ExtraVars = map[string]any{"non_admin_user": "true"}
		tgtApp.ExtraVars = map[string]any{"non_admin_user": "true"}

		By("Grant namespace-admin permissions to the non-admin user")
		kubectlSrc, cleanupSrc, err := SetupActiveNamespaceAdmin(
			scenario.KubectlSrc,
			scenario.KubectlSrcNonAdmin.Context,
			srcNamespace,
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanupSrc)
		kubectlTgt, cleanupTgt, err := SetupActiveNamespaceAdmin(
			scenario.KubectlTgt,
			scenario.KubectlTgtNonAdmin.Context,
			tgtNamespace,
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanupTgt)

		DeferCleanup(func() {
			By("Clean up MongoDB applications and namespaces")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
			for _, namespace := range []string{srcNamespace, tgtNamespace} {
				if _, err := scenario.KubectlSrc.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=120s"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q: %v", namespace, err)
				}
			}
		})

		By("Deploy and validate source MongoDB")
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

		By("Seed known MongoDB data and record the source document count")
		srcPodName, err := GetPodNameByLabel(kubectlSrc, srcNamespace, "name="+appName)
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectlSrc.Run(
			"exec", srcPodName, "-n", srcNamespace, "--",
			"mongosh", "sampledb",
			"--eval", `db.test_db.insertMany([{"a":1,"b":2},{"c":3,"d":4}]); print("seeded:", db.test_db.countDocuments())`,
			"--quiet",
		)
		Expect(err).NotTo(HaveOccurred())
		sourceDocumentCount, err := MongoDocumentCount(kubectlSrc, srcNamespace, srcPodName)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceDocumentCount).To(BeNumerically(">", 0), "source MongoDB should contain seeded documents")

		By("Scale down source MongoDB before transferring its RWO PVC")
		Expect(kubectlSrc.ScaleDeploymentIfPresent(srcNamespace, appName, 0)).NotTo(HaveOccurred())
		_, err = kubectlSrc.Run("wait", "pod", "-n", srcNamespace, "-l", "name="+appName, "--for=delete", "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		By("Render the target MongoDB workload without deploying or reseeding it")
		exportOpts := ExportOptions{Namespace: srcNamespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Transfer the PVC to the target namespace on the same cluster")
		nodeIP, err := GetClusterNodeIP(scenario.SrcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(runner.TransferPVC(TransferPVCOptions{
			SourceContext:   srcApp.Context,
			TargetContext:   tgtApp.Context,
			PVCName:         pvcName,
			PVCNamespaceMap: fmt.Sprintf("%s:%s", srcNamespace, tgtNamespace),
			Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvcName, tgtNamespace, nodeIP),
		})).NotTo(HaveOccurred())

		By("Verify the destination PVC is bound and transfer helpers are gone")
		destinationPVC, err := GetPVC(tgtApp.Context, tgtNamespace, pvcName)
		Expect(err).NotTo(HaveOccurred())
		Expect(destinationPVC.Status.Phase).To(Equal(corev1.ClaimBound))
		AssertNoTransferPVCLeftovers(kubectlSrc, []string{srcNamespace, tgtNamespace}, pvcName)

		By("Apply MongoDB in the target namespace using the transferred PVC")
		Expect(ApplyOutputToTargetWithNamespaceRemapNonAdmin(kubectlTgt, srcNamespace, tgtNamespace, paths.OutputDir)).NotTo(HaveOccurred())
		Expect(kubectlTgt.ScaleDeployment(tgtNamespace, appName, 1)).NotTo(HaveOccurred())

		By("Verify destination MongoDB contains the source data")
		var targetPodName string
		Eventually(func() error {
			podName, err := GetPodNameByLabel(kubectlTgt, tgtNamespace, "name="+appName)
			if err != nil {
				return err
			}
			targetPodName = podName
			return nil
		}, "2m", "10s").Should(Succeed())
		Eventually(func() (int, error) {
			return MongoDocumentCount(kubectlTgt, tgtNamespace, targetPodName)
		}, "5m", "10s").Should(Equal(sourceDocumentCount),
			"destination MongoDB document count should match the source after same-cluster transfer")
	})
})
