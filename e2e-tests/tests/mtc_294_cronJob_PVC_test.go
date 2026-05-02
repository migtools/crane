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

var _ = Describe("[BUG #330][MTC-294] CronJob with attached PVC migration as non-admin user", func() {
	It("[BUG #330][MTC-294] Should migrate a cronjob and its attached PVC as a namespace-admin user", Label("BUG #330", "tier0"), func() {
		appName := "cronjob"
		namespace := "mtc-294-ns"
		expectedLogSubstring := fmt.Sprintf("Hello! from namespace %s", namespace)

		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)

		if scenario.KubectlSrcNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for this test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for this test")
		}

		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin

		srcApp.ExtraVars = map[string]any{
			"ext_app_name":           appName,
			"non_admin_user":         "true",
			"with_deploy_with_pvc":   "true",
			"with_validate_with_pvc": "true",
		}

		tgtApp.ExtraVars = map[string]any{
			"ext_app_name":         appName,
			"non_admin_user":       "true",
			"with_deploy_with_pvc": "true",
		}

		By("Grant namespace-admin permissions to non-admin user on source and target")
		kubectlSrcNonAdmin, _, cleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Delete test namespace on source and target (best effort)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=false"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(cleanup)

		By("Prepare source app (CronJob + PVC)")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		By("Wait for at least one job pod to succeed and write to PVC")
		waitForLatestCronPod := func(k KubectlRunner) string {
			var podName string
			Eventually(func() string {
				out, err := k.Run(
					"get", "pod",
					"-n", namespace,
					"-l", "cronowner="+appName,
					"--sort-by=.metadata.creationTimestamp",
					"-o", "jsonpath={.items[*].metadata.name}",
				)
				if err != nil {
					return ""
				}
				pods := strings.Fields(out)
				if len(pods) == 0 {
					return ""
				}
				podName = pods[len(pods)-1]
				return podName
			}, "3m", "10s").ShouldNot(BeEmpty())
			Eventually(func() string {
				out, err := k.Run(
					"get", "pod", podName,
					"-n", namespace,
					"-o", "jsonpath={.status.phase}",
				)
				if err != nil {
					return ""
				}
				return out
			}, "3m", "10s").Should(Equal("Succeeded"))
			return podName
		}

		assertPodLogsContain := func(k KubectlRunner, podName, substr string) {
			Eventually(func() string {
				out, err := k.Run("logs", podName, "-n", namespace)
				if err != nil {
					return ""
				}
				return out
			}, "2m", "10s").Should(ContainSubstring(substr))
		}

		srcPodName := waitForLatestCronPod(scenario.KubectlSrc)
		log.Printf("First job pod on source: %s\n", srcPodName)
		assertPodLogsContain(scenario.KubectlSrc, srcPodName, expectedLogSubstring)
		log.Printf("Source job pod wrote expected log to PVC\n")

		By("Suspend source CronJob before export")
		_, err = kubectlSrcNonAdmin.Run(
			"patch", "cronjob", appName,
			"-n", namespace,
			"-p", `{"spec":{"suspend":true}}`,
		)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() string {
			out, err := kubectlSrcNonAdmin.Run(
				"get", "cronjob", appName,
				"-n", namespace,
				"-o", "jsonpath={.spec.suspend}",
			)
			if err != nil {
				return ""
			}
			return out
		}, "1m", "5s").Should(Equal("true"))
		log.Printf("CronJob %s suspended on source\n", appName)

		By("List PVCs in source namespace")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).NotTo(BeEmpty(), "expected at least one PVC in namespace %q", srcApp.Namespace)
		log.Printf("Found %d PVC(s) in namespace %q\n", len(pvcs), srcApp.Namespace)
		for _, pvc := range pvcs {
			log.Printf("  PVC: %s\n", pvc.Name)
		}
		pvcName := pvcs[0].Name
		log.Printf("Using PVC name for data integrity check: %s\n", pvcName)

		runner.WorkDir = paths.TempDir
		By("Run crane export/transform/apply pipeline as non-admin")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, srcApp.Namespace, paths)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Transfer PVC from source to target")
		// TODO(https://github.com/migtools/crane/issues/330): switch back to non-admin contexts
		// (srcApp.Context, tgtApp.Context) once crane transfer-pvc correctly handles
		// namespace-admin credentials on Linux.
		tgtIP, err := GetClusterNodeIP(scenario.TgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Target cluster IP: %s\n", tgtIP)

		for _, pvc := range pvcs {
			opts := TransferPVCOptions{
				SourceContext:   scenario.SrcApp.Context,
				TargetContext:   scenario.TgtApp.Context,
				PVCName:         pvc.Name,
				PVCNamespaceMap: fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
				Endpoint:        "nginx-ingress",
				IngressClass:    "nginx",
				Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvc.Name, srcApp.Namespace, tgtIP),
			}
			log.Printf("Transferring PVC %s -> namespace %s on target\n", pvc.Name, tgtApp.Namespace)
			Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred())
			log.Printf("PVC transfer complete: %s\n", pvc.Name)
		}

		By("Verify PVC exists and is Bound on target")
		for _, pvc := range pvcs {
			Eventually(func() string {
				out, err := scenario.KubectlTgt.Run(
					"get", "pvc", pvc.Name,
					"-n", tgtApp.Namespace,
					"-o", "jsonpath={.status.phase}",
				)
				if err != nil {
					return ""
				}
				return out
			}, "2m", "5s").Should(Equal("Bound"), "expected PVC %q to be Bound on target", pvc.Name)
		}

		By("Apply rendered manifests to target as non-admin")
		log.Printf("Applying manifests from %s to namespace %s\n", paths.OutputDir, tgtApp.Namespace)
		Expect(ApplyOutputToTargetNonAdmin(scenario.KubectlTgtNonAdmin	, paths.OutputDir)).NotTo(HaveOccurred())
		log.Printf("Manifests applied to target\n")

		By("Verify CronJob landed on target with correct schedule")
		Eventually(func() string {
			out, err := scenario.KubectlTgt.Run(
				"get", "cronjob", appName,
				"-n", namespace,
				"-o", "jsonpath={.spec.schedule}",
			)
			if err != nil {
				return ""
			}
			return out
		}, "1m", "5s").Should(Equal("*/1 * * * *"))
		log.Printf("CronJob %s confirmed on target with correct schedule\n", appName)

		By("Verify PVC data was transferred intact by running a reader pod on target")
		_, err = scenario.KubectlTgt.Run(
			"run", "pvc-reader",
			"-n", namespace,
			"--image=busybox",
			"--restart=Never",
			fmt.Sprintf(`--overrides={
				"spec": {
					"containers": [{
						"name": "pvc-reader",
						"image": "busybox",
						"command": ["sh", "-c", "cat /data/log.txt || echo FILE_NOT_FOUND"],
						"volumeMounts": [{"name":"data","mountPath":"/data"}]
					}],
					"volumes": [{"name":"data","persistentVolumeClaim":{"claimName":"%s"}}],
					"restartPolicy": "Never"
				}
			}`, pvcName),
		)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() string {
			out, err := scenario.KubectlTgt.Run(
				"get", "pod", "pvc-reader",
				"-n", namespace,
				"-o", "jsonpath={.status.phase}",
			)
			if err != nil {
				return ""
			}
			return out
		}, "2m", "5s").Should(Or(Equal("Succeeded"), Equal("Failed")))

		pvcReaderLogs, err := scenario.KubectlTgt.Run("logs", "pvc-reader", "-n", namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcReaderLogs).To(ContainSubstring(expectedLogSubstring),
			"expected PVC log.txt to contain data written on source cluster")
		log.Printf("PVC data integrity confirmed, source log entries present on target\n")

		_, _ = scenario.KubectlTgt.Run("delete", "pod", "pvc-reader", "-n", namespace, "--ignore-not-found=true")

		By("Unsuspend CronJob on target and verify it fires")
		_, err = scenario.KubectlTgt.Run(
			"patch", "cronjob", appName,
			"-n", namespace,
			"-p", `{"spec":{"suspend":false}}`,
		)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("CronJob %s unsuspended on target\n", appName)

		tgtPodName := waitForLatestCronPod(scenario.KubectlTgt)
		log.Printf("First job pod on target: %s\n", tgtPodName)
		assertPodLogsContain(scenario.KubectlTgt, tgtPodName, expectedLogSubstring)
		log.Printf("Target CronJob fired and wrote expected log, migration validated successfully\n")
	})
})