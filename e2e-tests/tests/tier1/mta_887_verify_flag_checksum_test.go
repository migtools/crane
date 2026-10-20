package e2e

import (
	"fmt"
	"log"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("transfer-pvc --verify flag", func() {
	It("[MTA-887] verify flag causes checksum-based rsync to detect content corruption missed by mtime+size comparison",
		Label("tier1", "pvc-transfer"), func() {
			const (
				appName         = "pvc"
				pvcName         = "data-pvc"
				originalContent = "original-data!!"
				tamperedContent = "tampered-data!!"
				knownMtime      = "2024-01-01 00:00:00"
			)
			namespace := "mta-887-verify"

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
				"pvc_name":       pvcName,
			}
			tgtApp.ExtraVars = map[string]any{
				"non_admin_user": "true",
				"pvc_name":       pvcName,
			}

			By("Grant namespace-admin permissions to non-admin users on source and target")
			kubectlSrc, kubectlTgt, cleanup, err := SetupActiveKubectlRunners(scenario, namespace)
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
			paths, err := NewScenarioPaths("crane-mta-887-*")
			Expect(err).NotTo(HaveOccurred())
			runner.WorkDir = paths.TempDir

			DeferCleanup(func() {
				if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
					log.Printf("cleanup apps/tempdir: %v", err)
				}
			})

			By("Deploy the source PVC app")
			log.Printf("Deploying source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
			Expect(srcApp.Deploy()).NotTo(HaveOccurred())
			log.Printf("Source app %s deployed successfully\n", srcApp.Name)

			By("Seed source PVC: write known file at fixed mtime and wait for pod completion")
			Expect(kubectlSrc.ApplyYAMLSpec(writePodManifest("mta-887-seed", namespace, pvcName, originalContent, knownMtime), namespace)).NotTo(HaveOccurred())
			_, err = kubectlSrc.Run("wait", "--for=jsonpath={.status.phase}=Succeeded", "pod/mta-887-seed", "-n", namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())
			log.Printf("Seed pod completed: /data/check.txt = %q at mtime %s\n", originalContent, knownMtime)

			tgtIP, err := GetClusterNodeIP(scenario.TgtApp.Context)
			Expect(err).NotTo(HaveOccurred())
			baseOpts := TransferPVCOptions{
				SourceContext:       srcApp.Context,
				TargetContext:       tgtApp.Context,
				PVCName:             pvcName,
				PVCNamespaceMap:     fmt.Sprintf("%s:%s", namespace, namespace),
				Subdomain:           fmt.Sprintf("%s.%s.%s.nip.io", pvcName, namespace, tgtIP),
				DisableCloudStorage: true,
			}

			// ── Phase 1: Transfer WITH --verify (happy path) ──────────────────────────
			By("Transfer PVC with --verify and confirm correct data arrives on target")
			log.Printf("Running transfer-pvc with --verify for PVC %s\n", pvcName)
			opts1 := baseOpts
			opts1.Verify = true
			Expect(runner.TransferPVC(opts1)).NotTo(HaveOccurred())
			log.Printf("transfer-pvc (--verify) completed\n")

			phase1Content := readFromVerifyPod(kubectlTgt, namespace, pvcName, "mta-887-phase1-check")
			Expect(phase1Content).To(Equal(originalContent),
				"target PVC must contain original content after transfer with --verify")
			log.Printf("Phase 1: target content %q matches source\n", phase1Content)

			// ── Phase 2: Corrupt target (same size + same forced mtime) ───────────────
			By("Corrupt target PVC: write equal-length tampered content at the same fixed mtime and wait for pod completion")
			Expect(len(tamperedContent)).To(Equal(len(originalContent)),
				"tampered content must be the same byte length as original to fool mtime+size comparison")
			Expect(kubectlTgt.ApplyYAMLSpec(writePodManifest("mta-887-corrupt-1", namespace, pvcName, tamperedContent, knownMtime), namespace)).NotTo(HaveOccurred())
			_, err = kubectlTgt.Run("wait", "--for=jsonpath={.status.phase}=Succeeded", "pod/mta-887-corrupt-1", "-n", namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())

			By("Confirm corruption was applied on target")
			corrupt1Content := readFromVerifyPod(kubectlTgt, namespace, pvcName, "mta-887-corrupt-1-check")
			Expect(corrupt1Content).To(Equal(tamperedContent))
			log.Printf("Phase 2: target file corrupted to %q (same size, same mtime as source)\n", corrupt1Content)

			// ── Phase 3: Re-transfer WITHOUT --verify → corruption NOT detected ────────
			By("Re-transfer without --verify: rsync uses mtime+size comparison and skips the corrupted file")
			log.Printf("Running transfer-pvc WITHOUT --verify for PVC %s\n", pvcName)
			opts2 := baseOpts
			opts2.Verify = false
			Expect(runner.TransferPVC(opts2)).NotTo(HaveOccurred())
			log.Printf("transfer-pvc (no --verify) completed\n")

			By("Confirm corruption persists: without --verify, rsync skipped the file (mtime+size matched)")
			phase3Content := readFromVerifyPod(kubectlTgt, namespace, pvcName, "mta-887-phase3-check")
			Expect(phase3Content).To(Equal(tamperedContent),
				"without --verify, rsync skips the file because mtime+size match source: corruption is NOT detected")
			log.Printf("Phase 3: target still has tampered content %q — corruption undetected (expected)\n", phase3Content)

			// ── Phase 4: Re-corrupt and transfer WITH --verify → corruption DETECTED ──
			By("Re-corrupt target with the same tampered content and fixed mtime and wait for pod completion")
			Expect(kubectlTgt.ApplyYAMLSpec(writePodManifest("mta-887-corrupt-2", namespace, pvcName, tamperedContent, knownMtime), namespace)).NotTo(HaveOccurred())
			_, err = kubectlTgt.Run("wait", "--for=jsonpath={.status.phase}=Succeeded", "pod/mta-887-corrupt-2", "-n", namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())

			By("Confirm re-corruption was applied on target")
			corrupt2Content := readFromVerifyPod(kubectlTgt, namespace, pvcName, "mta-887-corrupt-2-check")
			Expect(corrupt2Content).To(Equal(tamperedContent))
			log.Printf("Phase 4: target re-corrupted to %q\n", corrupt2Content)

			By("Re-transfer WITH --verify: rsync uses checksums, detects the mismatch, and re-copies the original content")
			log.Printf("Running transfer-pvc WITH --verify for PVC %s\n", pvcName)
			opts3 := baseOpts
			opts3.Verify = true
			Expect(runner.TransferPVC(opts3)).NotTo(HaveOccurred())
			log.Printf("transfer-pvc (--verify) completed\n")

			By("Confirm --verify detected the corruption and restored the original content")
			phase4Content := readFromVerifyPod(kubectlTgt, namespace, pvcName, "mta-887-phase4-check")
			Expect(phase4Content).To(Equal(originalContent),
				"with --verify, rsync detects the checksum mismatch and re-copies: original content must be restored")
			log.Printf("MTA-887: target content %q restored — --verify detected and fixed the corruption\n", phase4Content)
		})
})

// writePodManifest returns a one-shot pod spec that writes content to
// /data/check.txt at the given mtime and exits (restartPolicy: Never).
// Wait with --for=jsonpath={.status.phase}=Succeeded to guarantee the
// file operations completed before reading.
func writePodManifest(podName, namespace, pvcName, content, mtime string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  restartPolicy: Never
  containers:
  - name: write
    image: busybox:1.36
    command:
    - sh
    - -c
    - |
      set -eux
      printf '%s\n' > /data/check.txt
      touch -d '%s' /data/check.txt
    volumeMounts:
    - name: data
      mountPath: /data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: %s
`, podName, namespace, content, mtime, pvcName)
}

// readFromVerifyPod mounts pvcName in a short-lived pod, reads /data/check.txt
// via kubectl exec, deletes the pod, and returns the trimmed content.
// terminationGracePeriodSeconds: 1 keeps --wait=true deletion fast.
func readFromVerifyPod(k KubectlRunner, namespace, pvcName, podName string) string {
	GinkgoHelper()
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  restartPolicy: Never
  terminationGracePeriodSeconds: 1
  containers:
  - name: verify
    image: busybox:1.36
    command: ["sleep", "3600"]
    volumeMounts:
    - name: data
      mountPath: /data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: %s
`, podName, namespace, pvcName)
	Expect(k.ApplyYAMLSpec(manifest, namespace)).NotTo(HaveOccurred())
	_, err := k.Run("wait", "--for=condition=Ready", "pod/"+podName, "-n", namespace, "--timeout=120s")
	Expect(err).NotTo(HaveOccurred())
	content, err := ReadFileFromPod(k, namespace, podName, "/data/check.txt")
	Expect(err).NotTo(HaveOccurred())
	_, err = k.Run("delete", "pod", podName, "-n", namespace, "--wait=true")
	Expect(err).NotTo(HaveOccurred())
	return content
}
