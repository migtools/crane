package e2e

import (
	"fmt"
	"log"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Data validation with indirect migration of MySQL DB", func() {

	It("[BUG #213][MTA-807] Should validate data", Label("BUG #213", "tier0", "pvc-transfer"), func() {
		appName := "mysql"
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

		isOCP := scenario.KubectlSrc.IsOpenShift()
		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
			"has_scc":        isOCP,
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
			"has_scc":        isOCP,
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

		By("Deploy and validate source MySQL app")
		log.Printf("Deploying %s in namespace %s on source cluster", appName, namespace)
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())
		log.Printf("Source app deployed successfully")
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
		log.Printf("Source validation output: pod=%s authors_count=%d md5_actual=%s md5_expected=%s", srcPodName, sourceAuthorsCount, sourceMD5Actual, sourceMD5Expected)
		Expect(sourceMD5Actual).To(Equal(sourceMD5Expected), "source test-data checksum should match its md5 file")
		log.Printf("Source fingerprints: authors=%d md5=%s", sourceAuthorsCount, sourceMD5Actual)

		By("Quiesce source app before export")
		Expect(kubectlSrcNonAdmin.ScaleDeploymentIfPresent(srcApp.Namespace, srcApp.Name, 0)).NotTo(HaveOccurred())

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}

		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})
		By("List pvcs in the namespace")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).NotTo(BeEmpty(), "expected at least one pvc in namespace %q", srcApp.Namespace)
		log.Printf("Found %d pvcs in namespace %q", len(pvcs), srcApp.Namespace)
		for _, pvc := range pvcs {
			log.Printf("Found pvc %s in namespace %q\n", pvc.Name, pvc.Namespace)
		}

		By("Wait for source quiesce to stabilize before export")
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
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		runner.WorkDir = paths.TempDir
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

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
		Expect(VerifyPVCsExistByName(pvcs, tgtpvcs)).NotTo(HaveOccurred())
		log.Printf("Found %d PVCs in target namespace %q", len(tgtpvcs), tgtApp.Namespace)

		By("Pre-flight: verify target PVCs are schedulable")
		for _, pvc := range tgtpvcs {
			Expect(VerifyPVCSchedulable(scenario.KubectlTgt.Context, tgtApp.Namespace, pvc.Name)).
				NotTo(HaveOccurred())
		}

		By("Verify transferred PVCs contain data")
		for _, pvc := range tgtpvcs {
			mountPath := "/var/lib/mysql"
			if strings.Contains(pvc.Name, "data1") {
				mountPath = "/test-data"
			}
			Expect(VerifyPVCHasData(kubectlTgtNonAdmin, tgtApp.Namespace, pvc.Name, mountPath)).NotTo(HaveOccurred())
		}

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", tgtApp.Namespace, paths.OutputDir)
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app")
		log.Printf("Scaling target deployment(s) with label app=%s to 1\n", appName)
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())

		By("Validate target application")
		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
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

		targetAuthorsCount, err := MySQLAuthorsCount(kubectlTgtNonAdmin, tgtApp.Namespace, tgtPodName)
		Expect(err).NotTo(HaveOccurred())
		targetMD5Actual, targetMD5Expected, err := MySQLTestDataMD5(kubectlTgtNonAdmin, tgtApp.Namespace, tgtPodName)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Target validation output: pod=%s authors_count=%d md5_actual=%s md5_expected=%s", tgtPodName, targetAuthorsCount, targetMD5Actual, targetMD5Expected)
		Expect(targetMD5Actual).To(Equal(targetMD5Expected), "target test-data checksum should match its md5 file")

		Expect(targetAuthorsCount).To(Equal(sourceAuthorsCount), "authors count should match between source and target")
		Expect(targetMD5Actual).To(Equal(sourceMD5Actual), "test-data md5 should match between source and target")
		log.Printf("Target fingerprints: authors=%d md5=%s", targetAuthorsCount, targetMD5Actual)
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)
	})
})
