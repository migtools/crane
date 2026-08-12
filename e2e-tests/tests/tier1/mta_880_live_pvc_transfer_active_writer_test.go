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

var _ = Describe("Live PVC transfer with an active writer", func() {
	It("[MTA-880] should transfer a PVC while an active writer is still writing (no quiesce)", Label("tier1", "pvc-transfer"), func() {
		appName := "pvc-live-writer"
		namespace := "mta-880-live-pvc"
		pvcName := appName + "-pvc"
		verifyPodName := "verify-live-pvc"
		const (
			dataMountPath = "/data"
			counterFile   = "counter.txt"
			logFile       = "live.log"
		)

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
		// RWO is enough here: transfer-pvc pins the rsync client to the writer's node.
		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
			"app_name":       appName,
			"access_mode":    "ReadWriteOnce",
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
			"app_name":       appName,
			"access_mode":    "ReadWriteOnce",
		}

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
		paths, err := NewScenarioPaths("crane-mta-880-*")
		Expect(err).NotTo(HaveOccurred())
		runner.WorkDir = paths.TempDir

		DeferCleanup(func() {
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup apps/tempdir: %v", err)
			}
		})

		By("Deploy a source app that continuously writes to its PVC (do not quiesce)")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		By("Confirm the writer workload is still running (no scale-down)")
		readyReplicas, err := kubectlSrcNonAdmin.Run(
			"get", "deploy", appName, "-n", namespace,
			"-o", "jsonpath={.status.readyReplicas}",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(StripKubectlWarnings(readyReplicas))).To(Equal("1"))

		By("List PVCs created by the live writer app")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).NotTo(BeEmpty(), "expected at least one PVC in source namespace %q", srcApp.Namespace)
		_, err = kubectlSrcNonAdmin.Run("get", "pvc", pvcName, "-n", namespace)
		Expect(err).NotTo(HaveOccurred())

		writerPod, err := GetPodNameByLabel(kubectlSrcNonAdmin, namespace, "app="+appName)
		Expect(err).NotTo(HaveOccurred())
		Expect(writerPod).NotTo(BeEmpty())

		By("Capture a pre-transfer snapshot of actively written data")
		preCounterRaw, err := readFileFromPod(kubectlSrcNonAdmin, namespace, writerPod, dataMountPath+"/"+counterFile)
		Expect(err).NotTo(HaveOccurred())
		preCounter, err := strconv.Atoi(preCounterRaw)
		Expect(err).NotTo(HaveOccurred(), "pre-transfer counter should be an integer, got %q", preCounterRaw)
		Expect(preCounter).To(BeNumerically(">", 0))
		preLog, err := readFileFromPod(kubectlSrcNonAdmin, namespace, writerPod, dataMountPath+"/"+logFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(preLog).NotTo(BeEmpty())
		log.Printf("MTA-880 pre-transfer snapshot: counter=%d log_bytes≈%d\n", preCounter, len(preLog))

		By("While writes are ongoing, run crane transfer-pvc to migrate the PVC")
		tgtIP, err := GetClusterNodeIP(scenario.TgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		opts := TransferPVCOptions{
			SourceContext:   srcApp.Context,
			TargetContext:   tgtApp.Context,
			PVCName:         pvcName,
			PVCNamespaceMap: fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
			Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvcName, namespace, tgtIP),
		}
		Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred(),
			"transfer-pvc must complete without crashing even with an active writer (consistency is not guaranteed)")

		By("Confirm the source writer was still active around transfer completion")
		readyAfterTransfer, err := kubectlSrcNonAdmin.Run(
			"get", "deploy", appName, "-n", namespace,
			"-o", "jsonpath={.status.readyReplicas}",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(StripKubectlWarnings(readyAfterTransfer))).To(Equal("1"))

		By("Capture a late source snapshot close to transfer completion")
		writerPodLate, err := GetPodNameByLabel(kubectlSrcNonAdmin, namespace, "app="+appName)
		Expect(err).NotTo(HaveOccurred())
		Expect(writerPodLate).NotTo(BeEmpty())
		lateCounterRaw, err := readFileFromPod(kubectlSrcNonAdmin, namespace, writerPodLate, dataMountPath+"/"+counterFile)
		Expect(err).NotTo(HaveOccurred())
		lateCounter, err := strconv.Atoi(lateCounterRaw)
		Expect(err).NotTo(HaveOccurred(), "late source counter should be an integer, got %q", lateCounterRaw)
		Expect(lateCounter).To(BeNumerically(">=", preCounter),
			"writer should still be advancing (or at least not regress) around transfer completion")
		lateLog, err := readFileFromPod(kubectlSrcNonAdmin, namespace, writerPodLate, dataMountPath+"/"+logFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(lateLog).NotTo(BeEmpty())
		log.Printf("MTA-880 late source snapshot: counter=%d log_bytes≈%d\n", lateCounter, len(lateLog))

		By("Verify the destination PVC exists on target")
		tgtPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(VerifyPVCsExistByName(pvcs, tgtPVCs)).NotTo(HaveOccurred())

		By("Mount the transferred PVC in a verifier pod on the target cluster")
		Expect(kubectlTgtNonAdmin.ApplyYAMLSpec(verifyPodManifest(namespace, verifyPodName, pvcName), namespace)).NotTo(HaveOccurred())
		_, err = kubectlTgtNonAdmin.Run("wait", "--for=condition=Ready", "pod/"+verifyPodName, "-n", namespace, "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		By("Compare destination data against the late source snapshot (document consistency; do not require a perfect match)")
		destCounterRaw, err := readFileFromPod(kubectlTgtNonAdmin, namespace, verifyPodName, dataMountPath+"/"+counterFile)
		Expect(err).NotTo(HaveOccurred())
		destCounter, err := strconv.Atoi(destCounterRaw)
		Expect(err).NotTo(HaveOccurred(), "destination counter should be readable/non-corrupt, got %q", destCounterRaw)
		Expect(destCounter).To(BeNumerically(">", 0), "destination counter must be present and usable")

		destLog, err := readFileFromPod(kubectlTgtNonAdmin, namespace, verifyPodName, dataMountPath+"/"+logFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(destLog).NotTo(BeEmpty(), "destination live.log must be present and readable")

		consistency := "partially inconsistent"
		switch {
		case destCounter == lateCounter && destLog == lateLog:
			consistency = "fully consistent with late source snapshot"
		case destCounter <= lateCounter && strings.HasPrefix(lateLog, destLog):
			consistency = "partially consistent (destination appears to be an earlier prefix of source)"
		case destCounter <= lateCounter:
			consistency = "partially inconsistent (counter not ahead of source; log diverged)"
		default:
			consistency = "unexpected (destination counter ahead of late source snapshot)"
		}
		log.Printf(
			"MTA-880 observed transfer behavior with active writer: consistency=%s pre=%d late=%d dest=%d dest_log_bytes≈%d late_log_bytes≈%d (perfect consistency is not required without quiesce; transfer must not crash/corrupt)\n",
			consistency, preCounter, lateCounter, destCounter, len(destLog), len(lateLog),
		)
		if destCounter > lateCounter {
			log.Printf("MTA-880 note: dest counter (%d) > late source counter (%d); documenting only — Polarion does not require perfect consistency without quiesce\n", destCounter, lateCounter)
		}
	})
})
