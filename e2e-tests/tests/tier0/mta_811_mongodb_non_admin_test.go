package e2e

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// mongoDocumentCount returns the number of documents in sampledb.test_db via
// mongosh exec into the given pod.
func mongoDocumentCount(k KubectlRunner, namespace, podName string) (int, error) {
	out, err := k.Run(
		"exec", podName, "-n", namespace, "--",
		"mongosh", "sampledb",
		"--eval", "db.test_db.countDocuments()",
		"--quiet",
	)
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("failed to parse document count %q: %w", strings.TrimSpace(out), err)
	}
	return count, nil
}

var _ = Describe("MongoDB Migration", func() {
	It("[BUG #213][MTA-811] Should migrate a MongoDB resource with data intact as nonadmin user", Label("BUG #213", "tier0", "pvc-transfer"), func() {
		appName := "mongodb"
		namespace := appName
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

		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		By("Grant namespace admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupActiveKubectlRunners(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			By("Delete test namespace on source and target (wait for completion)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(cleanup)

		By("Deploy and validate source MongoDB app")
		log.Printf("Deploying %s in namespace %s on source cluster", appName, namespace)
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())
		log.Printf("Source app deployed successfully")

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts := ExportOptions{Namespace: namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		By("Seed test data into source MongoDB")
		srcPodName, err := kubectlSrcNonAdmin.Run(
			"get", "pod",
			"-n", namespace,
			"-l", "name=mongodb",
			"-o", "jsonpath={.items[0].metadata.name}",
		)
		Expect(err).NotTo(HaveOccurred())
		srcPodName = strings.TrimSpace(srcPodName)
		log.Printf("Source pod: %s", srcPodName)

		_, err = kubectlSrcNonAdmin.Run(
			"exec", srcPodName, "-n", namespace, "--",
			"mongosh", "sampledb",
			"--eval", `db.test_db.insertMany([{"a":1,"b":2},{"c":3,"d":4}]); print("seeded:", db.test_db.countDocuments())`,
			"--quiet",
		)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Test data seeded into source MongoDB")
		srcCount, err := mongoDocumentCount(kubectlSrcNonAdmin, namespace, srcPodName)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Source document count: %d", srcCount)

		By("Scale down source MongoDB deployment")
		Expect(kubectlSrcNonAdmin.ScaleDeploymentIfPresent(namespace, appName, 0)).NotTo(HaveOccurred())

		_, err = scenario.KubectlSrc.Run(
			"wait", "pod",
			"-n", namespace,
			"-l", "name=mongodb",
			"--for=delete",
			"--timeout=60s",
		)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Source deployment scaled down and pod terminated")

		By("Run crane export/transform/apply pipeline")
		runner.WorkDir = paths.TempDir
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s", namespace)

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s", namespace, paths.OutputDir)
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("List PVCs and transfer to target")
		pvcs, err := ListPVCs(namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).NotTo(BeEmpty(), "expected at least one PVC in namespace %q", namespace)

		tgtIP, err := GetClusterNodeIP(scenario.TgtApp.Context)
		Expect(err).NotTo(HaveOccurred())

		for _, pvc := range pvcs {
			log.Printf("Transferring PVC %s", pvc.Name)
			opts := TransferPVCOptions{
				SourceContext:   srcApp.Context,
				TargetContext:   tgtApp.Context,
				PVCName:         pvc.Name,
				PVCNamespaceMap: fmt.Sprintf("%s:%s", namespace, namespace),
				Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvc.Name, namespace, tgtIP),
			}
			Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred())
			log.Printf("PVC %s transferred successfully", pvc.Name)
		}

		By("Scale up target MongoDB deployment")
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
		log.Printf("Target deployment scaled up")

		By("Wait for target MongoDB pod to be ready")
		var tgtPodName string
		Eventually(func() error {
			podName, err := GetPodNameByLabel(kubectlTgtNonAdmin, namespace, "name="+appName)
			if err != nil {
				return err
			}
			tgtPodName = podName
			out, err := kubectlTgtNonAdmin.Run(
				"get", "pod", tgtPodName,
				"-n", namespace,
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
		log.Printf("Target MongoDB pod is ready: %s", tgtPodName)

		By("Verify data integrity on destination")
		Eventually(func() (int, error) {
			return mongoDocumentCount(kubectlTgtNonAdmin, namespace, tgtPodName)
		}, "2m", "10s").Should(BeNumerically("==", srcCount),
			"expected destination document count to match source after migration")

		tgtCount, err := mongoDocumentCount(kubectlTgtNonAdmin, namespace, tgtPodName)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Destination document count: %d — migration verified", tgtCount)
	})
})
