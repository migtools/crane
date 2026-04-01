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

var _ = Describe("[MTC-273] Cronjob Quiesced", func() {
	It("Should suspend a cronjob and apply to target cluster and unsuspend it", Label("tier0"), func() {
		appName := "cronjob"
		cronJobName := "external-cronjob"
		namespace := "cronjob"
		expectedHelloLog := fmt.Sprintf("Hello! from namespace %s", namespace)

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

		waitForLatestCronPod := func(k KubectlRunner) string {
			var podName string
			Eventually(func() string {
				out, err := k.Run(
					"get", "pod",
					"-n", namespace,
					"-l", "cronowner="+cronJobName,
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
			}, "2m", "10s").ShouldNot(BeEmpty())
			return podName
		}

		assertPodLogsHello := func(k KubectlRunner, podName string) {
			Eventually(func() string {
				podLogs, err := k.Run("logs", podName, "-n", namespace)
				if err != nil {
					return ""
				}
				return podLogs
			}, "2m", "10s").Should(ContainSubstring(expectedHelloLog))
		}

		patchCronSuspend := func(k KubectlRunner, suspend bool) {
			_, err := k.Run(
				"patch", "cronjob", cronJobName,
				"-n", namespace,
				"-p", fmt.Sprintf(`{"spec":{"suspend":%t}}`, suspend),
			)
			Expect(err).NotTo(HaveOccurred())
		}

		assertCronSuspendState := func(k KubectlRunner, expected string) {
			suspended, err := k.Run("get", "cronjob", cronJobName, "-n", namespace, "-o", "jsonpath={.spec.suspend}")
			Expect(err).NotTo(HaveOccurred())
			Expect(suspended).To(Equal(expected))
		}

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)
		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		By("Verify source cronjob runs and emits expected log")
		sourcePodName := waitForLatestCronPod(kubectlSrc)
		assertPodLogsHello(kubectlSrc, sourcePodName)

		By("Suspend source cronjob")
		patchCronSuspend(kubectlSrc, true)
		log.Printf("Cronjob %s suspended successfully", cronJobName)
		By("Verify source cronjob is suspended")
		assertCronSuspendState(kubectlSrc, "true")
		log.Printf("Cronjob %s is suspended", cronJobName)

		// Run crane export/transform/apply pipeline
		runner := scenario.Crane
		runner.WorkDir = paths.TempDir
		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, srcApp.Namespace, paths)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		By("Verify the cronjob is in a suspended state on target")
		assertCronSuspendState(kubectlTgt, "true")
		log.Printf("Cronjob %s is suspended on target", cronJobName)
		By("Unsuspend the cronjob")
		patchCronSuspend(kubectlTgt, false)
		log.Printf("Cronjob %s unsuspended successfully", cronJobName)
		By("Verify the cronjob is unsuspended on target")
		assertCronSuspendState(kubectlTgt, "false")
		log.Printf("Cronjob %s is unsuspended on target", cronJobName)
		By("Verify target cronjob runs and emits expected log")
		targetPodName := waitForLatestCronPod(kubectlTgt)
		assertPodLogsHello(kubectlTgt, targetPodName)
	})
}) //End of [MTC-273] Cronjob Quiesced
