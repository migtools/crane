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

// fixPVCPermissionsViaJob spawns a temporary busybox pod to make mountPath
// world-readable/executable so that crane's rsync pod (uid 1000) can traverse
// directories written by MongoDB (uid 999, mode 0700).
//
// This must run AFTER the MongoDB deployment is scaled to zero — the MongoDB
// pod must not be running when this executes, because WiredTiger will write new
// checkpoint files (e.g. WiredTiger.turtle) with mode 700 after the chmod if
// MongoDB is still active, and rsync will silently skip those files.
//
// TODO: Remove once crane rsync pods support supplemental groups.
// See https://github.com/migtools/crane/issues/213.
func fixPVCPermissionsViaJob(k KubectlRunner, namespace, pvcName, mountPath string) error {
	podSpec := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: fix-pvc-perms
  namespace: %s
spec:
  restartPolicy: Never
  containers:
  - name: fix
    image: busybox
    command: ["chmod", "-R", "o+rx", "%s"]
    volumeMounts:
    - name: data
      mountPath: %s
    securityContext:
      runAsUser: 0
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: %s
`, namespace, mountPath, mountPath, pvcName)

	if err := k.ApplyYAMLSpec(podSpec, namespace); err != nil {
		return fmt.Errorf("failed to create fix-pvc-perms pod: %w", err)
	}
	if _, err := k.Run(
		"wait", "pod", "fix-pvc-perms",
		"-n", namespace,
		"--for=jsonpath={.status.phase}=Succeeded",
		"--timeout=60s",
	); err != nil {
		return fmt.Errorf("fix-pvc-perms pod did not succeed: %w", err)
	}
	// Verify it actually succeeded and didn't just time out in a non-Failed state
	phase, err := k.Run(
		"get", "pod", "fix-pvc-perms",
		"-n", namespace,
		"-o", "jsonpath={.status.phase}",
	)
	if err != nil {
		return fmt.Errorf("failed to get fix-pvc-perms pod phase: %w", err)
	}
	if strings.TrimSpace(phase) != "Succeeded" {
		return fmt.Errorf("fix-pvc-perms pod ended in phase %q, expected Succeeded", phase)
	}
	if _, err := k.Run("delete", "pod", "fix-pvc-perms", "-n", namespace); err != nil {
		return fmt.Errorf("failed to delete fix-pvc-perms pod: %w", err)
	}
	return nil
}

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
	It("[BUG #213][MTC-292] Should migrate a MongoDB resource with data intact as nonadmin user", Label("BUG #213", "tier0"), func() {
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

		if scenario.KubectlSrcNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin stateful migration test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin stateful migration test")
		}
		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin

		srcApp.ExtraVars = map[string]string{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = map[string]string{
			"non_admin_user": "true",
		}

		By("Grant namespace admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
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
		// Must scale down BEFORE fixing permissions — if MongoDB is still running
		// after chmod, WiredTiger will write new checkpoint files (e.g.
		// WiredTiger.turtle) with mode 700, which rsync (uid 1000) cannot read,
		// causing a silent empty transfer.
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

		// todo: remove this once crane rsync pods support supplemental groups.
		By("[BUG #213] Fix source PVC permissions after scale-down")
		Expect(fixPVCPermissionsViaJob(scenario.KubectlSrc, namespace, "mongodb-data", "/data/db")).NotTo(HaveOccurred())
		log.Printf("Source PVC permissions fixed")

		By("Run crane export/transform/apply pipeline")
		runner.WorkDir = paths.TempDir
		Expect(RunCranePipelineWithChecks(runner, namespace, paths)).NotTo(HaveOccurred())
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
				Endpoint:        "nginx-ingress",
				IngressClass:    "nginx",
				Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvc.Name, namespace, tgtIP),
			}
			Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred())
			log.Printf("PVC %s transferred successfully", pvc.Name)
		}

		By("Scale up target MongoDB deployment")
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
		log.Printf("Target deployment scaled up")

		By("Wait for target MongoDB pod to be ready")
		_, err = scenario.KubectlTgt.Run(
			"wait", "pod",
			"-n", namespace,
			"-l", "name=mongodb",
			"--for=condition=Ready",
			"--timeout=2m",
		)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Target MongoDB pod is ready")

		By("Verify data integrity on destination")
		tgtPodName, err := kubectlTgtNonAdmin.Run(
			"get", "pod",
			"-n", namespace,
			"-l", "name=mongodb",
			"-o", "jsonpath={.items[0].metadata.name}",
		)
		Expect(err).NotTo(HaveOccurred())
		tgtPodName = strings.TrimSpace(tgtPodName)
		log.Printf("Target pod: %s", tgtPodName)

		Eventually(func() (int, error) {
			return mongoDocumentCount(kubectlTgtNonAdmin, namespace, tgtPodName)
		}, "2m", "10s").Should(BeNumerically("==", srcCount),
			"expected destination document count to match source after migration")

		tgtCount, err := mongoDocumentCount(kubectlTgtNonAdmin, namespace, tgtPodName)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Destination document count: %d — migration verified", tgtCount)
	})
})