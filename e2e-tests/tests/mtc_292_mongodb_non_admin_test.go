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

// TODO: Remove once crane rsync pods support supplemental groups.
// fixPVCPermissions makes mountPath world-readable/executable so that crane's
// rsync pod (uid 1000) can traverse directories written by MongoDB (uid 999,
// mode 0700). This is a temporary workaround until crane transfer-pvc supports
// supplemental groups. See https://github.com/migtools/crane/issues/213.
func fixPVCPermissions(k KubectlRunner, namespace, _ /* pvcName */, mountPath string) error {
	podName, err := k.Run(
		"get", "pod",
		"-n", namespace,
		"-l", "name=mongodb",
		"-o", "jsonpath={.items[0].metadata.name}",
	)
	if err != nil {
		return err
	}
	podName = strings.TrimSpace(podName)
	_, err = k.Run("exec", podName, "-n", namespace, "--", "chmod", "-R", "o+rx", mountPath)
	return err
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

// fixTargetPVCOwnership restores file ownership on the target PVC to uid/gid 999
// (MongoDB's uid) after crane's rsync transfers them as uid 1000.
// TODO: Remove once crane rsync pods support supplemental groups.
func fixTargetPVCOwnership(k KubectlRunner, namespace, pvcName, mountPath string) error {
	podName := fmt.Sprintf("fix-perms-%s", pvcName)
	podSpec := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  restartPolicy: Never
  containers:
  - name: fix-perms
    image: busybox
    command: ["chown", "-R", "999:999", "%s"]
    volumeMounts:
    - name: data
      mountPath: %s
    securityContext:
      runAsUser: 0
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: %s
`, podName, namespace, mountPath, mountPath, pvcName)

	_, err := k.RunWithStdin(podSpec, "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("failed to create fix-permissions pod: %w", err)
	}
	defer k.Run("delete", "pod", podName, "-n", namespace, "--ignore-not-found=true")

	_, err = k.Run("wait", "pod", podName, "-n", namespace,
		"--for=jsonpath={.status.phase}=Succeeded", "--timeout=60s")
	if err != nil {
		return fmt.Errorf("fix-permissions pod did not complete successfully: %w", err)
	}
	return nil
}

var _ = Describe("MongoDB Migration", func() {
	It("[MTC-292] Should migrate a MongoDB resource with data intact as nonadmin user", Label("tier0"), func() {
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
			Skip("source-nonadmin-context is required for non-admin stateless migration test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin stateless migration test")
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
			By("Delete test namespace on source and target (best effort)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=false"); err != nil {
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

		By("Fix source PVC permissions before transfer")
		// TODO: Remove once crane rsync pods support supplemental groups.
		Expect(fixPVCPermissions(kubectlSrcNonAdmin, namespace, "mongodb-data", "/data/db")).NotTo(HaveOccurred())
		log.Printf("Source PVC permissions fixed")

		By("Scale down source MongoDB deployment")
		Expect(kubectlSrcNonAdmin.ScaleDeploymentIfPresent(namespace, appName, 0)).NotTo(HaveOccurred())
		log.Printf("Source deployment scaled down")

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

		By("Fix target PVC ownership after transfer")
		// TODO: Remove once crane rsync pods support supplemental groups.
		for _, pvc := range pvcs {
			Expect(fixTargetPVCOwnership(scenario.KubectlTgt, namespace, pvc.Name, "/data/db")).NotTo(HaveOccurred())
			log.Printf("Target PVC %s ownership fixed", pvc.Name)
		}

		By("Scale up target MongoDB")
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
		log.Printf("Target deployment scaled up")

		By("Validate target app is running")
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target app validated successfully")

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
		}, "2m", "10s").Should(BeNumerically("==", 4),
			"expected 4 documents on destination after migration")

		tgtCount, err := mongoDocumentCount(kubectlTgtNonAdmin, namespace, tgtPodName)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Destination document count: %d — migration verified", tgtCount)
	})
})