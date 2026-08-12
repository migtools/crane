package e2e

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Concurrent multi-PVC transfer for the same app", func() {
	It("[MTA-876] Verify multiple PVCs for the same app transfer correctly and efficiently when run concurrently", Label("tier1", "pvc-transfer"), func() {
		appName := "8pvc-app"
		namespace := "mta-876-concurrent-pvc"
		volumeCount := 8

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
		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
			"file_size": 1,
		}
		tgtApp.ExtraVars = map[string]any{"non_admin_user": "true"}

		By("Grant namespace-admin permissions to non-admin users on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupActiveKubectlRunners(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(cleanup)

		runner := scenario.CraneNonAdmin
		paths, err := NewScenarioPaths("crane-mta-876-*")
		Expect(err).NotTo(HaveOccurred())
		runner.WorkDir = paths.TempDir

		DeferCleanup(func() {
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup apps/tempdir: %v", err)
			}
		})

		By("Deploy a source app with multiple PVCs, each seeded with distinct known data, on a single node")
		Expect(srcApp.Deploy()).NotTo(HaveOccurred())
		Expect(srcApp.Validate()).NotTo(HaveOccurred())

		srcPodName, err := GetPodNameByLabel(kubectlSrcNonAdmin, srcApp.Namespace, "app="+appName)
		Expect(err).NotTo(HaveOccurred())

		By("List PVCs created by the source app")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).To(HaveLen(volumeCount), "expected %d PVCs in namespace %q", volumeCount, srcApp.Namespace)

		By("Capture the source MD5 checksum for each volume and confirm each holds distinct data")
		srcMD5s := make(map[string]string, volumeCount)
		for i := 1; i <= volumeCount; i++ {
			vol := fmt.Sprintf("volume%d", i)
			md5, err := md5sumFile(kubectlSrcNonAdmin, srcApp.Namespace, srcPodName, fmt.Sprintf("/mnt/%s/random-data", vol))
			Expect(err).NotTo(HaveOccurred())
			Expect(md5).NotTo(BeEmpty())
			srcMD5s[vol] = md5
		}
		distinctHashes := make(map[string]bool, volumeCount)
		for _, h := range srcMD5s {
			distinctHashes[h] = true
		}
		Expect(distinctHashes).To(HaveLen(volumeCount), "expected each volume to hold distinct data")

		By("Launch a separate crane transfer-pvc invocation for each PVC at the same time")
		tgtIP, err := GetClusterNodeIP(tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())

		durations := make([]time.Duration, volumeCount)
		transferErrs := make([]error, volumeCount)
		var wg sync.WaitGroup
		start := time.Now()
		for i := 0; i < volumeCount; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				vol := fmt.Sprintf("volume%d", i+1)
				opts := TransferPVCOptions{
					SourceContext:   srcApp.Context,
					TargetContext:   tgtApp.Context,
					PVCName:         vol,
					PVCNamespaceMap: fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
					Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", vol, srcApp.Namespace, tgtIP),
				}
				t0 := time.Now()
				transferErrs[i] = runner.TransferPVC(opts)
				durations[i] = time.Since(t0)
			}(i)
		}
		wg.Wait()
		totalElapsed := time.Since(start)

		By("Verify all invocations started and completed without resource-creation collisions or errors")
		var sumDurations time.Duration
		for i, terr := range transferErrs {
			Expect(terr).NotTo(HaveOccurred(), "concurrent transfer-pvc for volume%d failed", i+1)
			sumDurations += durations[i]
			log.Printf("volume%d transfer took %s\n", i+1, durations[i])
		}

		By("Verify total wall-clock time reflects real concurrency, not serialized transfers")
		minDuration := durations[0]
		maxDuration := durations[0]
		for _, d := range durations[1:] {
			if d < minDuration {
				minDuration = d
			}
			if d > maxDuration {
				maxDuration = d
			}
		}
		log.Printf("total wall clock: %s, fastest single transfer: %s, slowest single transfer: %s, sum of individual durations: %s\n",
			totalElapsed, minDuration, maxDuration, sumDurations)
		Expect(totalElapsed).To(BeNumerically("<", sumDurations),
			"running %d transfers concurrently should take less than the sum of their individual durations", volumeCount)
		// Compare against the FASTEST transfer, not the slowest. Every goroutine's
		// timer starts at the same instant, so the slowest transfer's own duration
		// is always ~= totalElapsed whether the transfers ran concurrently or were
		// fully serialized (it's still "running", from its own t0, for the whole
		// queue ahead of it) — comparing against it can't tell the two cases apart.
		// The fastest transfer's duration stays small if it truly ran in parallel,
		// and grows close to totalElapsed if everything was serialized (queued
		// behind the others), so it's the correct baseline for this check.
		concurrencyTolerance := 3
		Expect(totalElapsed).To(BeNumerically("<", minDuration*time.Duration(concurrencyTolerance)),
			"total wall-clock time (%s) should stay within %dx the fastest single transfer (%s), not balloon toward serialized execution (sum: %s)",
			totalElapsed, concurrencyTolerance, minDuration, sumDurations)

		By("Verify each destination PVC exists on target")
		tgtPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(VerifyPVCsExistByName(pvcs, tgtPVCs)).NotTo(HaveOccurred())

		By("Mount all destination PVCs in a verifier pod and confirm each has correct, uncontaminated data")
		const verifierPod = "mta-876-pvc-verifier"
		Expect(kubectlTgtNonAdmin.ApplyYAMLSpec(multiVolumePodManifest(verifierPod, tgtApp.Namespace, volumeCount, nil), tgtApp.Namespace)).NotTo(HaveOccurred())
		DeferCleanup(func() {
			if _, err := kubectlTgtNonAdmin.Run("delete", "pod", verifierPod, "-n", tgtApp.Namespace, "--ignore-not-found", "--wait=true"); err != nil {
				log.Printf("cleanup verifier pod %q: %v", verifierPod, err)
			}
		})
		_, err = kubectlTgtNonAdmin.Run("wait", "--for=condition=Ready", "pod/"+verifierPod, "-n", tgtApp.Namespace, "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		assertVolumesMatchSource(kubectlTgtNonAdmin, tgtApp.Namespace, verifierPod, volumeCount, srcMD5s,
			"%s data on target should match source with no cross-contamination")
		_, err = kubectlTgtNonAdmin.Run("delete", "pod", verifierPod, "-n", tgtApp.Namespace, "--ignore-not-found", "--wait=true")
		Expect(err).NotTo(HaveOccurred())

		By("Deploy the app on target using the migrated PVCs and confirm it starts with correct data")
		appLabels := map[string]string{"app": appName}
		Expect(kubectlTgtNonAdmin.ApplyYAMLSpec(multiVolumePodManifest(appName, tgtApp.Namespace, volumeCount, appLabels), tgtApp.Namespace)).NotTo(HaveOccurred())
		_, err = kubectlTgtNonAdmin.Run("wait", "--for=condition=Ready", "pod/"+appName, "-n", tgtApp.Namespace, "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		assertVolumesMatchSource(kubectlTgtNonAdmin, tgtApp.Namespace, appName, volumeCount, srcMD5s,
			"app on target should read correct %s data after mounting migrated PVCs")
	})
})

func assertVolumesMatchSource(k KubectlRunner, namespace, pod string, volumeCount int, srcMD5s map[string]string, msgFormat string) {
	GinkgoHelper()
	for i := 1; i <= volumeCount; i++ {
		vol := fmt.Sprintf("volume%d", i)
		md5, err := md5sumFile(k, namespace, pod, fmt.Sprintf("/mnt/%s/random-data", vol))
		Expect(err).NotTo(HaveOccurred(), "md5sum of %s in pod %q (namespace %q) failed", vol, pod, namespace)
		Expect(md5).To(Equal(srcMD5s[vol]), msgFormat+" (pod %q)", vol, pod)
	}
}

func multiVolumePodManifest(podName, namespace string, volumeCount int, labels map[string]string) string {
	var labelLines strings.Builder
	for k, v := range labels {
		labelLines.WriteString(fmt.Sprintf("    %s: %s\n", k, v))
	}
	labelsBlock := ""
	if labelLines.Len() > 0 {
		labelsBlock = "  labels:\n" + labelLines.String()
	}

	var mounts, volumes strings.Builder
	for i := 1; i <= volumeCount; i++ {
		vol := fmt.Sprintf("volume%d", i)
		mounts.WriteString(fmt.Sprintf("    - name: %s\n      mountPath: /mnt/%s\n", vol, vol))
		volumes.WriteString(fmt.Sprintf("  - name: %s\n    persistentVolumeClaim:\n      claimName: %s\n", vol, vol))
	}

	return fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
%sspec:
  restartPolicy: Never
  containers:
  - name: busybox
    image: busybox:1.36
    command: ["/bin/sh", "-c", "sleep 3600"]
    volumeMounts:
%s  volumes:
%s`, podName, namespace, labelsBlock, mounts.String(), volumes.String())
}
