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
				seedPodName     = "mta-887-seed"
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

			By("Seed source PVC with a known file at a fixed mtime using a temporary pod")
			seedManifest := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  restartPolicy: Never
  containers:
  - name: seed
    image: busybox:1.36
    command:
    - sh
    - -c
    - |
      set -eux
      printf '%s\n' > /data/check.txt
      touch -d '%s' /data/check.txt
      sleep 3600
    volumeMounts:
    - name: data
      mountPath: /data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: %s
`, seedPodName, namespace, originalContent, knownMtime, pvcName)
			Expect(kubectlSrc.ApplyYAMLSpec(seedManifest, namespace)).NotTo(HaveOccurred())
			_, err = kubectlSrc.Run("wait", "--for=condition=Ready", "pod/"+seedPodName, "-n", namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())

			By("Confirm source file content and mtime")
			srcFileContent, err := ReadFileFromPod(kubectlSrc, namespace, seedPodName, "/data/check.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(srcFileContent).To(Equal(originalContent))
			log.Printf("Source file content: %q\n", srcFileContent)

			By("Delete seed pod so the source PVC is unattached")
			_, err = kubectlSrc.Run("delete", "pod", seedPodName, "-n", namespace, "--wait=true")
			Expect(err).NotTo(HaveOccurred())

			tgtIP, err := GetClusterNodeIP(scenario.TgtApp.Context)
			Expect(err).NotTo(HaveOccurred())
			baseOpts := TransferPVCOptions{
				SourceContext:        srcApp.Context,
				TargetContext:        tgtApp.Context,
				PVCName:              pvcName,
				PVCNamespaceMap:      fmt.Sprintf("%s:%s", namespace, namespace),
				Subdomain:            fmt.Sprintf("%s.%s.%s.nip.io", pvcName, namespace, tgtIP),
				DisableCloudStorage:  true,
			}

			// ── Phase 1: Transfer WITH --verify (happy path) ──────────────────────────
			By("Transfer PVC with --verify and confirm correct data arrives on target")
			log.Printf("Running transfer-pvc with --verify for PVC %s\n", pvcName)
			opts := baseOpts
			opts.Verify = true
			Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred())
			log.Printf("transfer-pvc (--verify) completed\n")

			By("Verify target PVC contains the original content after --verify transfer")
			const verifyPod1 = "mta-887-verify-1"
			Expect(kubectlTgt.ApplyYAMLSpec(VerifyPodManifest(namespace, verifyPod1, pvcName), namespace)).NotTo(HaveOccurred())
			_, err = kubectlTgt.Run("wait", "--for=condition=Ready", "pod/"+verifyPod1, "-n", namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())
			tgtContent1, err := ReadFileFromPod(kubectlTgt, namespace, verifyPod1, "/data/check.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(tgtContent1).To(Equal(originalContent),
				"target PVC must contain original content after transfer with --verify")
			log.Printf("Phase 1: target content %q matches source %q\n", tgtContent1, originalContent)
			_, err = kubectlTgt.Run("delete", "pod", verifyPod1, "-n", namespace, "--wait=true")
			Expect(err).NotTo(HaveOccurred())

			// ── Phase 2: Corrupt target (same size, same forced mtime as source) ──────
			By("Corrupt target PVC: write equal-length tampered content with the same fixed mtime")
			Expect(len(tamperedContent)).To(Equal(len(originalContent)),
				"tampered content must be the same byte length as original to fool mtime+size comparison")
			corrupt1Name := "mta-887-corrupt-1"
			corrupt1Manifest := corruptPodManifest(corrupt1Name, namespace, pvcName, tamperedContent, knownMtime)
			Expect(kubectlTgt.ApplyYAMLSpec(corrupt1Manifest, namespace)).NotTo(HaveOccurred())
			_, err = kubectlTgt.Run("wait", "--for=condition=Ready", "pod/"+corrupt1Name, "-n", namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())

			By("Confirm corruption was applied on target")
			corruptedContent, err := ReadFileFromPod(kubectlTgt, namespace, corrupt1Name, "/data/check.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(corruptedContent).To(Equal(tamperedContent))
			log.Printf("Phase 2: target file corrupted to %q (same size, same mtime as source)\n", corruptedContent)
			_, err = kubectlTgt.Run("delete", "pod", corrupt1Name, "-n", namespace, "--wait=true")
			Expect(err).NotTo(HaveOccurred())

			// ── Phase 3: Re-transfer WITHOUT --verify → corruption NOT detected ────────
			By("Re-transfer without --verify: rsync uses mtime+size comparison and skips the corrupted file")
			log.Printf("Running transfer-pvc WITHOUT --verify for PVC %s\n", pvcName)
			opts2 := baseOpts
			opts2.Verify = false
			Expect(runner.TransferPVC(opts2)).NotTo(HaveOccurred())
			log.Printf("transfer-pvc (no --verify) completed\n")

			By("Confirm corruption persists: without --verify, rsync skipped the file (mtime+size matched)")
			const verifyPod2 = "mta-887-verify-2"
			Expect(kubectlTgt.ApplyYAMLSpec(VerifyPodManifest(namespace, verifyPod2, pvcName), namespace)).NotTo(HaveOccurred())
			_, err = kubectlTgt.Run("wait", "--for=condition=Ready", "pod/"+verifyPod2, "-n", namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())
			tgtContent2, err := ReadFileFromPod(kubectlTgt, namespace, verifyPod2, "/data/check.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(tgtContent2).To(Equal(tamperedContent),
				"without --verify, rsync skips the file because mtime+size match source: corruption is NOT detected")
			log.Printf("Phase 3: target still has tampered content %q — corruption undetected (expected)\n", tgtContent2)
			_, err = kubectlTgt.Run("delete", "pod", verifyPod2, "-n", namespace, "--wait=true")
			Expect(err).NotTo(HaveOccurred())

			// ── Phase 4: Re-corrupt and transfer WITH --verify → corruption DETECTED ──
			By("Re-corrupt target with the same tampered content and fixed mtime")
			corrupt2Name := "mta-887-corrupt-2"
			corrupt2Manifest := corruptPodManifest(corrupt2Name, namespace, pvcName, tamperedContent, knownMtime)
			Expect(kubectlTgt.ApplyYAMLSpec(corrupt2Manifest, namespace)).NotTo(HaveOccurred())
			_, err = kubectlTgt.Run("wait", "--for=condition=Ready", "pod/"+corrupt2Name, "-n", namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())
			reCorruptedContent, err := ReadFileFromPod(kubectlTgt, namespace, corrupt2Name, "/data/check.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(reCorruptedContent).To(Equal(tamperedContent))
			log.Printf("Phase 4: target re-corrupted to %q\n", reCorruptedContent)
			_, err = kubectlTgt.Run("delete", "pod", corrupt2Name, "-n", namespace, "--wait=true")
			Expect(err).NotTo(HaveOccurred())

			By("Re-transfer WITH --verify: rsync uses checksums, detects the mismatch, and re-copies the original content")
			log.Printf("Running transfer-pvc WITH --verify for PVC %s\n", pvcName)
			opts3 := baseOpts
			opts3.Verify = true
			Expect(runner.TransferPVC(opts3)).NotTo(HaveOccurred())
			log.Printf("transfer-pvc (--verify) completed\n")

			By("Confirm --verify detected the corruption and restored the original content")
			const verifyPod3 = "mta-887-verify-3"
			Expect(kubectlTgt.ApplyYAMLSpec(VerifyPodManifest(namespace, verifyPod3, pvcName), namespace)).NotTo(HaveOccurred())
			_, err = kubectlTgt.Run("wait", "--for=condition=Ready", "pod/"+verifyPod3, "-n", namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())
			tgtContent3, err := ReadFileFromPod(kubectlTgt, namespace, verifyPod3, "/data/check.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(tgtContent3).To(Equal(originalContent),
				"with --verify, rsync detects the checksum mismatch and re-copies: original content must be restored")
			log.Printf("Phase 4: target content %q restored to original — --verify detected and fixed the corruption\n", tgtContent3)
			_, err = kubectlTgt.Run("delete", "pod", verifyPod3, "-n", namespace, "--wait=true")
			Expect(err).NotTo(HaveOccurred())

			log.Printf("MTA-887: --verify flag confirmed: mtime+size skipped corruption (%q→%q silently), --checksum detected it and restored %q\n",
				originalContent, tamperedContent, originalContent)
		})
})

// corruptPodManifest returns a pod spec that overwrites /data/check.txt with
// tamperedContent and sets its mtime to knownMtime, then stays alive for exec.
func corruptPodManifest(podName, namespace, pvcName, tamperedContent, knownMtime string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  restartPolicy: Never
  containers:
  - name: corrupt
    image: busybox:1.36
    command:
    - sh
    - -c
    - |
      set -eux
      printf '%s\n' > /data/check.txt
      touch -d '%s' /data/check.txt
      sleep 3600
    volumeMounts:
    - name: data
      mountPath: /data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: %s
`, podName, namespace, tamperedContent, knownMtime, pvcName)
}
