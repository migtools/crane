package e2e

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	mta898RedisPassword    = "PASSWORD"
	deltaBaselineKey       = "delta-test-baseline"
	deltaBaselineSizeBytes = 3_000_000
)

var _ = Describe("Stage and cutover migration flow", func() {
	It("[MTA-898] Verify stage + cutover migration flow", Label("tier0", "pvc-transfer"), func() {
		appName := "redis"
		namespace := "mta-898-stage-cutover"

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

		paths, err := NewScenarioPaths("crane-mta-898-*")
		Expect(err).NotTo(HaveOccurred())
		runner := scenario.Crane
		runner.WorkDir = paths.TempDir
		exportOpts := ExportOptions{Namespace: namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}

		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		By("Deploy a source app with a PVC seeded with initial known data")
		Expect(srcApp.Deploy()).NotTo(HaveOccurred())
		Expect(srcApp.Validate()).NotTo(HaveOccurred())

		srcPodName, err := GetPodNameByLabel(kubectlSrc, srcApp.Namespace, "name="+appName)
		Expect(err).NotTo(HaveOccurred())

		By("List the PVC created by the source app")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).To(HaveLen(1), "expected exactly one PVC in namespace %q", srcApp.Namespace)
		pvcName := pvcs[0].Name

		initial, err := redisGet(kubectlSrc, srcApp.Namespace, srcPodName, appName, "mytestkey")
		Expect(err).NotTo(HaveOccurred())
		Expect(initial).To(ContainSubstring("Hello world"))

		By("Seed a multi-MB baseline value so a genuine incremental sync is distinguishable from a full re-copy")
		Expect(redisSetRandomBlob(kubectlSrc, srcApp.Namespace, srcPodName, appName, deltaBaselineKey, deltaBaselineSizeBytes)).NotTo(HaveOccurred())
		Expect(redisSave(kubectlSrc, srcApp.Namespace, srcPodName, appName)).NotTo(HaveOccurred())
		baselineLen, err := redisStrlen(kubectlSrc, srcApp.Namespace, srcPodName, appName, deltaBaselineKey)
		Expect(err).NotTo(HaveOccurred())
		Expect(baselineLen).To(BeNumerically(">", int64(deltaBaselineSizeBytes)))

		By("Create target namespace")
		Expect(kubectlTgt.CreateNamespace(tgtApp.Namespace)).NotTo(HaveOccurred())

		tgtIP, err := GetClusterNodeIP(tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		transferOpts := TransferPVCOptions{
			SourceContext:   srcApp.Context,
			TargetContext:   tgtApp.Context,
			PVCName:         pvcName,
			PVCNamespaceMap: fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
			Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvcName, srcApp.Namespace, tgtIP),
		}

		By("Run the first stage transfer-pvc while the app is still running on source")
		Expect(runner.TransferPVC(transferOpts)).NotTo(HaveOccurred())

		By("Verify the source app is unaffected and still running after the stage sync")
		srcPhase, err := kubectlSrc.Run("get", "pod", srcPodName, "-n", srcApp.Namespace, "-o", "jsonpath={.status.phase}")
		Expect(err).NotTo(HaveOccurred())
		Expect(srcPhase).To(Equal("Running"))

		By("Verify the initial data landed on target via a throwaway redis instance on the migrated PVC")
		const verifierPod = "mta-898-redis-verifier"
		verifierOpts := VerifierPodOptions{
			Name:      verifierPod,
			Namespace: tgtApp.Namespace,
			Image:     "redis:latest",
			Command:   []string{"redis-server", "--requirepass", mta898RedisPassword},
			Volumes:   []PodVolumeMount{{PVCName: pvcName, MountPath: "/data"}},
		}
		DeferCleanup(func() {
			if err := DeleteVerifierPod(kubectlTgt, tgtApp.Namespace, verifierPod); err != nil {
				log.Printf("cleanup verifier pod %q: %v", verifierPod, err)
			}
		})
		Expect(DeployVerifierPod(kubectlTgt, verifierOpts)).NotTo(HaveOccurred())
		verifyValue, err := redisGet(kubectlTgt, tgtApp.Namespace, verifierPod, "verifier", "mytestkey")
		Expect(err).NotTo(HaveOccurred())
		Expect(verifyValue).To(Equal(initial))
		Expect(DeleteVerifierPod(kubectlTgt, tgtApp.Namespace, verifierPod)).NotTo(HaveOccurred())

		By("Insert new data on source while the app is still running")
		Expect(redisSet(kubectlSrc, srcApp.Namespace, srcPodName, appName, "stage1key", "stage1-value")).NotTo(HaveOccurred())
		Expect(redisSave(kubectlSrc, srcApp.Namespace, srcPodName, appName)).NotTo(HaveOccurred())

		By("Verify the new data is present on source and the original data set is untouched")
		stage1OnSource, err := redisGet(kubectlSrc, srcApp.Namespace, srcPodName, appName, "stage1key")
		Expect(err).NotTo(HaveOccurred())
		Expect(stage1OnSource).To(Equal("stage1-value"))
		mytestkeyAfterInsert1, err := redisGet(kubectlSrc, srcApp.Namespace, srcPodName, appName, "mytestkey")
		Expect(err).NotTo(HaveOccurred())
		Expect(mytestkeyAfterInsert1).To(Equal(initial))

		By("Run the second stage transfer-pvc while capturing the rsync client's raw log for delta verification")
		rsyncLogCh := make(chan string, 1)
		go captureRsyncClientLog(kubectlSrc, srcApp.Namespace, pvcName, rsyncLogCh)
		Expect(runner.TransferPVC(transferOpts)).NotTo(HaveOccurred())
		rsyncLog := <-rsyncLogCh

		By("Verify only the newly added data was transferred, not a full re-copy of the PVC")
		literalBytes, err := literalDataBytes(rsyncLog)
		Expect(err).NotTo(HaveOccurred(), "expected to find rsync's stats summary in the client log:\n%s", rsyncLog)
		Expect(literalBytes).To(BeNumerically("<", float64(deltaBaselineSizeBytes)/10),
			"second sync sent %.0f bytes of literal (new) data; expected well under the %d-byte baseline blob if only the new key was sent, not a full re-copy",
			literalBytes, deltaBaselineSizeBytes)

		By("Verify no data is missing: both the initial and new keys are present on target")
		Expect(DeployVerifierPod(kubectlTgt, verifierOpts)).NotTo(HaveOccurred())
		mytestkeyAfterStage2, err := redisGet(kubectlTgt, tgtApp.Namespace, verifierPod, "verifier", "mytestkey")
		Expect(err).NotTo(HaveOccurred())
		Expect(mytestkeyAfterStage2).To(Equal(initial))
		stage1ValueOnTarget, err := redisGet(kubectlTgt, tgtApp.Namespace, verifierPod, "verifier", "stage1key")
		Expect(err).NotTo(HaveOccurred())
		Expect(stage1ValueOnTarget).To(Equal("stage1-value"))
		Expect(DeleteVerifierPod(kubectlTgt, tgtApp.Namespace, verifierPod)).NotTo(HaveOccurred())

		By("Insert a second new data set on source")
		Expect(redisSet(kubectlSrc, srcApp.Namespace, srcPodName, appName, "stage2key", "stage2-value")).NotTo(HaveOccurred())
		Expect(redisSave(kubectlSrc, srcApp.Namespace, srcPodName, appName)).NotTo(HaveOccurred())

		By("Verify the second new data set is present on source and prior data is untouched")
		stage2OnSource, err := redisGet(kubectlSrc, srcApp.Namespace, srcPodName, appName, "stage2key")
		Expect(err).NotTo(HaveOccurred())
		Expect(stage2OnSource).To(Equal("stage2-value"))
		mytestkeyAfterInsert2, err := redisGet(kubectlSrc, srcApp.Namespace, srcPodName, appName, "mytestkey")
		Expect(err).NotTo(HaveOccurred())
		Expect(mytestkeyAfterInsert2).To(Equal(initial))
		stage1AfterInsert2, err := redisGet(kubectlSrc, srcApp.Namespace, srcPodName, appName, "stage1key")
		Expect(err).NotTo(HaveOccurred())
		Expect(stage1AfterInsert2).To(Equal("stage1-value"))

		By("Quiesce the source app for cutover")
		Expect(kubectlSrc.ScaleDeploymentIfPresent(srcApp.Namespace, appName, 0)).NotTo(HaveOccurred())
		WaitForSourceQuiesce(kubectlSrc, srcApp.Namespace, "name="+appName, appName)

		By("Run the cutover pipeline in order: export, transform, final transfer-pvc sync, apply")
		Expect(runner.Export(exportOpts)).NotTo(HaveOccurred())
		Expect(runner.Transform(transformOpts)).NotTo(HaveOccurred())
		Expect(runner.TransferPVC(transferOpts)).NotTo(HaveOccurred())
		Expect(runner.Apply(applyOpts)).NotTo(HaveOccurred())

		By("Verify rendered output excludes the PVC: it is migrated separately via transfer-pvc")
		Expect(utils.AssertNoKindsInOutput(paths.OutputDir, []string{"PersistentVolumeClaim"})).NotTo(HaveOccurred())

		By("Apply rendered manifests to target and scale the app back up")
		Expect(ApplyOutputToTarget(kubectlTgt, tgtApp.Namespace, paths.OutputDir)).NotTo(HaveOccurred())
		Expect(kubectlTgt.ScaleDeployment(tgtApp.Namespace, appName, 1)).NotTo(HaveOccurred())
		Eventually(tgtApp.Validate, "5m", "10s").Should(Succeed())

		By("Validate all data inserted throughout the test is present on target")
		tgtPodName, err := GetPodNameByLabel(kubectlTgt, tgtApp.Namespace, "name="+appName)
		Expect(err).NotTo(HaveOccurred())

		finalMytestkey, err := redisGet(kubectlTgt, tgtApp.Namespace, tgtPodName, appName, "mytestkey")
		Expect(err).NotTo(HaveOccurred())
		Expect(finalMytestkey).To(Equal(initial))

		finalStage1, err := redisGet(kubectlTgt, tgtApp.Namespace, tgtPodName, appName, "stage1key")
		Expect(err).NotTo(HaveOccurred())
		Expect(finalStage1).To(Equal("stage1-value"))

		finalStage2, err := redisGet(kubectlTgt, tgtApp.Namespace, tgtPodName, appName, "stage2key")
		Expect(err).NotTo(HaveOccurred())
		Expect(finalStage2).To(Equal("stage2-value"))

		finalBaselineLen, err := redisStrlen(kubectlTgt, tgtApp.Namespace, tgtPodName, appName, deltaBaselineKey)
		Expect(err).NotTo(HaveOccurred())
		Expect(finalBaselineLen).To(Equal(baselineLen))
	})
})

func redisExec(k KubectlRunner, namespace, pod, container string, args ...string) (string, error) {
	cmdArgs := append([]string{"exec", pod, "-n", namespace, "-c", container, "--", "redis-cli", "-a", mta898RedisPassword}, args...)
	out, err := k.Run(cmdArgs...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(StripKubectlWarnings(out)), nil
}

func redisGet(k KubectlRunner, namespace, pod, container, key string) (string, error) {
	return redisExec(k, namespace, pod, container, "get", key)
}

func redisSet(k KubectlRunner, namespace, pod, container, key, value string) error {
	_, err := redisExec(k, namespace, pod, container, "set", key, value)
	return err
}

func redisSave(k KubectlRunner, namespace, pod, container string) error {
	_, err := redisExec(k, namespace, pod, container, "save")
	return err
}

func redisStrlen(k KubectlRunner, namespace, pod, container, key string) (int64, error) {
	out, err := redisExec(k, namespace, pod, container, "strlen", key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(out, 10, 64)
}

func redisSetRandomBlob(k KubectlRunner, namespace, pod, container, key string, sizeBytes int) error {
	shellCmd := fmt.Sprintf("head -c %d /dev/urandom | base64 | redis-cli -a %s -x set %s", sizeBytes, mta898RedisPassword, key)
	_, err := k.Run("exec", pod, "-n", namespace, "-c", container, "--", "/bin/sh", "-c", shellCmd)
	return err
}

var literalDataRegex = regexp.MustCompile(`Literal data: ([\d,.]+)\s*([KMGT]?) bytes`)

var byteUnitMultipliers = map[string]float64{
	"": 1, "K": 1_000, "M": 1_000_000, "G": 1_000_000_000, "T": 1_000_000_000_000,
}

func literalDataBytes(rsyncLog string) (float64, error) {
	matched := literalDataRegex.FindStringSubmatch(rsyncLog)
	if len(matched) < 3 {
		return 0, fmt.Errorf("could not find %q in rsync client log", "Literal data")
	}
	val, err := strconv.ParseFloat(strings.ReplaceAll(matched[1], ",", ""), 64)
	if err != nil {
		return 0, err
	}
	return val * byteUnitMultipliers[matched[2]], nil
}

func captureRsyncClientLog(k KubectlRunner, namespace, pvcName string, result chan<- string) {
	selector := fmt.Sprintf(
		"app.kubernetes.io/component=transfer-pvc,app.konveyor.io/created-for-pvc=%s",
		pvcName,
	)
	deadline := time.Now().Add(180 * time.Second)
	var lastLog string
	seenPod := false
	for time.Now().Before(deadline) {
		out, err := k.Run("get", "pods", "-n", namespace, "-l", selector, "-o", "name")
		if err != nil || strings.TrimSpace(out) == "" {
			if seenPod {
				break
			}
			time.Sleep(300 * time.Millisecond)
			continue
		}
		podName := strings.TrimPrefix(strings.Split(strings.TrimSpace(out), "\n")[0], "pod/")
		seenPod = true
		if podLog, err := k.Run("logs", podName, "-n", namespace, "-c", "rsync"); err == nil && podLog != "" {
			lastLog = podLog
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !seenPod {
		result <- fmt.Sprintf("no rsync-client pod found in namespace %q (selector %q) within the capture window", namespace, selector)
		return
	}
	result <- lastLog
}
