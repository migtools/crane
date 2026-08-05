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
		srcApp := scenario.SrcApp
		tgtApp := scenario.TgtApp
		kubectlSrc := scenario.KubectlSrc
		kubectlTgt := scenario.KubectlTgt

		// Keep the per-volume payload small (10MB instead of the role's 100MB
		// default) so the timing/concurrency assertions stay fast and stable in CI.
		srcApp.ExtraVars = map[string]any{"file_size": 1}

		paths, err := NewScenarioPaths("crane-mta-876-*")
		Expect(err).NotTo(HaveOccurred())
		runner := scenario.Crane
		runner.WorkDir = paths.TempDir

		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		By("Deploy a source app with multiple PVCs, each seeded with distinct known data, on a single node")
		Expect(srcApp.Deploy()).NotTo(HaveOccurred())
		Expect(srcApp.Validate()).NotTo(HaveOccurred())

		srcPodName, err := GetPodNameByLabel(kubectlSrc, srcApp.Namespace, "app="+appName)
		Expect(err).NotTo(HaveOccurred())

		By("List PVCs created by the source app")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).To(HaveLen(volumeCount), "expected %d PVCs in namespace %q", volumeCount, srcApp.Namespace)

		By("Capture the source MD5 checksum for each volume and confirm each holds distinct data")
		srcMD5s := make(map[string]string, volumeCount)
		for i := 1; i <= volumeCount; i++ {
			vol := fmt.Sprintf("volume%d", i)
			md5, err := md5sumFile(kubectlSrc, srcApp.Namespace, srcPodName, fmt.Sprintf("/mnt/%s/random-data", vol))
			Expect(err).NotTo(HaveOccurred())
			Expect(md5).NotTo(BeEmpty())
			srcMD5s[vol] = md5
		}
		distinctHashes := make(map[string]bool, volumeCount)
		for _, h := range srcMD5s {
			distinctHashes[h] = true
		}
		Expect(distinctHashes).To(HaveLen(volumeCount), "expected each volume to hold distinct data")

		By("Create target namespace")
		Expect(kubectlTgt.CreateNamespace(tgtApp.Namespace)).NotTo(HaveOccurred())

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
		maxDuration := durations[0]
		for _, d := range durations[1:] {
			if d > maxDuration {
				maxDuration = d
			}
		}
		log.Printf("total wall clock: %s, slowest single transfer: %s, sum of individual durations: %s\n",
			totalElapsed, maxDuration, sumDurations)
		Expect(totalElapsed).To(BeNumerically("<", sumDurations),
			"running %d transfers concurrently should take less than the sum of their individual durations", volumeCount)
		// Allow generous headroom for scheduling and endpoint-programming jitter
		// while still failing if the transfers were effectively serialized.
		concurrencyTolerance := 3
		Expect(totalElapsed).To(BeNumerically("<", maxDuration*time.Duration(concurrencyTolerance)),
			"total wall-clock time (%s) should stay within %dx the slowest single transfer (%s), not balloon toward the serialized sum (%s)",
			totalElapsed, concurrencyTolerance, maxDuration, sumDurations)

		By("Verify each destination PVC exists on target")
		tgtPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(VerifyPVCsExistByName(pvcs, tgtPVCs)).NotTo(HaveOccurred())

		By("Mount all destination PVCs in a verifier pod and confirm each has correct, uncontaminated data")
		const verifierPod = "mta-876-pvc-verifier"
		Expect(kubectlTgt.ApplyYAMLSpec(multiVolumePodManifest(verifierPod, tgtApp.Namespace, volumeCount, nil), tgtApp.Namespace)).NotTo(HaveOccurred())
		DeferCleanup(func() {
			if _, err := kubectlTgt.Run("delete", "pod", verifierPod, "-n", tgtApp.Namespace, "--ignore-not-found", "--wait=true"); err != nil {
				log.Printf("cleanup verifier pod %q: %v", verifierPod, err)
			}
		})
		_, err = kubectlTgt.Run("wait", "--for=condition=Ready", "pod/"+verifierPod, "-n", tgtApp.Namespace, "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		assertVolumesMatchSource(kubectlTgt, tgtApp.Namespace, verifierPod, volumeCount, srcMD5s,
			"%s data on target should match source with no cross-contamination")
		_, err = kubectlTgt.Run("delete", "pod", verifierPod, "-n", tgtApp.Namespace, "--ignore-not-found", "--wait=true")
		Expect(err).NotTo(HaveOccurred())

		By("Deploy the app on target using the migrated PVCs and confirm it starts with correct data")
		appLabels := map[string]string{"app": appName}
		Expect(kubectlTgt.ApplyYAMLSpec(multiVolumePodManifest(appName, tgtApp.Namespace, volumeCount, appLabels), tgtApp.Namespace)).NotTo(HaveOccurred())
		_, err = kubectlTgt.Run("wait", "--for=condition=Ready", "pod/"+appName, "-n", tgtApp.Namespace, "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		assertVolumesMatchSource(kubectlTgt, tgtApp.Namespace, appName, volumeCount, srcMD5s,
			"app on target should read correct %s data after mounting migrated PVCs")
	})
})

// assertVolumesMatchSource execs into pod and checks that volume1..volumeN's data
// each match the corresponding source checksum, failing with msgFormat (which takes
// the volume name) on the first mismatch.
func assertVolumesMatchSource(k KubectlRunner, namespace, pod string, volumeCount int, srcMD5s map[string]string, msgFormat string) {
	GinkgoHelper()
	for i := 1; i <= volumeCount; i++ {
		vol := fmt.Sprintf("volume%d", i)
		md5, err := md5sumFile(k, namespace, pod, fmt.Sprintf("/mnt/%s/random-data", vol))
		Expect(err).NotTo(HaveOccurred(), "md5sum of %s in pod %q (namespace %q) failed", vol, pod, namespace)
		Expect(md5).To(Equal(srcMD5s[vol]), msgFormat+" (pod %q)", vol, pod)
	}
}

// multiVolumePodManifest renders a single pod that mounts volumeCount PVCs
// named volume1..volumeN at /mnt/volumeN, matching the layout the 8pvc-app
// role uses on the source side. Passing labels lets the same manifest double
// as either a throwaway verifier pod (nil labels) or the redeployed app pod
// (labelled to match the original Deployment's selector).
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
