package e2e

import (
	"fmt"
	"log"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Unattached PVC transfer", func() {
	It("[MTA-879] Migrates an unattached PVC with seeded data", Label("tier1", "pvc-transfer"), func() {
		appName := "pvc"
		namespace := "mta-879-unattached-pvc"
		pvcName := "data-pvc"
		seedPodName := "seed-pvc"
		verifyPodName := "verify-pvc"

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
		paths, err := NewScenarioPaths("crane-mta-879-*")
		Expect(err).NotTo(HaveOccurred())
		runner.WorkDir = paths.TempDir

		exportOpts := ExportOptions{Namespace: namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}

		DeferCleanup(func() {
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup apps/tempdir: %v", err)
			}
		})

		By("Deploy the source pvc app from k8s-apps-deployer")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		By("List PVCs created by the source pvc app")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).NotTo(BeEmpty(), "expected at least one PVC in source namespace %q", srcApp.Namespace)
		_, err = kubectlSrcNonAdmin.Run("get", "pvc", pvcName, "-n", namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Seed known data into the PVC with a temporary pod")
		Expect(kubectlSrcNonAdmin.ApplyYAMLSpec(seedPodManifest(namespace, seedPodName, pvcName), namespace)).NotTo(HaveOccurred())
		_, err = kubectlSrcNonAdmin.Run("wait", "--for=condition=Ready", "pod/"+seedPodName, "-n", namespace, "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		sourceHello, err := readFileFromPod(kubectlSrcNonAdmin, namespace, seedPodName, "/data/hello.txt")
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceHello).To(Equal("hello-from-source"))

		sourceNested, err := readFileFromPod(kubectlSrcNonAdmin, namespace, seedPodName, "/data/testdir/nested.txt")
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceNested).To(Equal("unattached-pvc-check"))

		sourceTimestamp, err := readFileFromPod(kubectlSrcNonAdmin, namespace, seedPodName, "/data/timestamp.txt")
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceTimestamp).NotTo(BeEmpty())

		By("Delete the temporary pod so the PVC becomes unattached")
		_, err = kubectlSrcNonAdmin.Run("delete", "pod", seedPodName, "-n", namespace, "--wait=true")
		Expect(err).NotTo(HaveOccurred())

		By("Confirm the PVC is not referenced by any running workload")
		sourcePods, err := kubectlSrcNonAdmin.Run("get", "pods", "-n", namespace, "-o", "name")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(StripKubectlWarnings(sourcePods))).To(BeEmpty())

		sourceWorkloads, err := kubectlSrcNonAdmin.Run("get", "deploy,sts,job,cronjob", "-n", namespace, "-o", "name")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(StripKubectlWarnings(sourceWorkloads))).To(BeEmpty())

		By("Run crane export, transform, and apply for the namespace")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verify export captured the unattached PVC")
		exportFiles, err := utils.ListFilesRecursivelyAsList(paths.ExportDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.Join(exportFiles, "\n")).To(ContainSubstring("PersistentVolumeClaim"))

		By("Verify output excludes PVC manifests because PVCs are migrated separately")
		Expect(utils.AssertNoKindsInOutput(paths.OutputDir, []string{"PersistentVolumeClaim"})).NotTo(HaveOccurred())

		By("Transfer the unattached PVC explicitly")
		tgtIP, err := GetClusterNodeIP(scenario.TgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		opts := TransferPVCOptions{
			SourceContext:   srcApp.Context,
			TargetContext:   tgtApp.Context,
			PVCName:         pvcName,
			PVCNamespaceMap: fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
			Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvcName, namespace, tgtIP),
		}
		Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred())

		By("Verify the destination PVC exists and is bound on target")
		Expect(tgtApp.Validate()).NotTo(HaveOccurred())
		tgtPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(VerifyPVCsExistByName(pvcs, tgtPVCs)).NotTo(HaveOccurred())

		By("Apply rendered manifests to the target cluster")
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Mount the transferred PVC in a verifier pod on the target cluster")
		Expect(kubectlTgtNonAdmin.ApplyYAMLSpec(verifyPodManifest(namespace, verifyPodName, pvcName), namespace)).NotTo(HaveOccurred())
		_, err = kubectlTgtNonAdmin.Run("wait", "--for=condition=Ready", "pod/"+verifyPodName, "-n", namespace, "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		By("Verify the target PVC contents match the source data")
		targetHello, err := readFileFromPod(kubectlTgtNonAdmin, namespace, verifyPodName, "/data/hello.txt")
		Expect(err).NotTo(HaveOccurred())
		Expect(targetHello).To(Equal(sourceHello))

		targetNested, err := readFileFromPod(kubectlTgtNonAdmin, namespace, verifyPodName, "/data/testdir/nested.txt")
		Expect(err).NotTo(HaveOccurred())
		Expect(targetNested).To(Equal(sourceNested))

		targetTimestamp, err := readFileFromPod(kubectlTgtNonAdmin, namespace, verifyPodName, "/data/timestamp.txt")
		Expect(err).NotTo(HaveOccurred())
		Expect(targetTimestamp).To(Equal(sourceTimestamp))
	})
})

func readFileFromPod(k KubectlRunner, namespace, podName, filePath string) (string, error) {
	out, err := k.Run("exec", "-n", namespace, podName, "--", "cat", filePath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func seedPodManifest(namespace, podName, pvcName string) string {
	return fmt.Sprintf(`apiVersion: v1
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
          mkdir -p /data/testdir
          echo 'hello-from-source' > /data/hello.txt
          echo 'unattached-pvc-check' > /data/testdir/nested.txt
          date -u +"%%Y-%%m-%%dT%%H:%%M:%%SZ" > /data/timestamp.txt
          sleep 3600
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: %s
`, podName, namespace, pvcName)
}

func verifyPodManifest(namespace, podName, pvcName string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  restartPolicy: Never
  containers:
    - name: verify
      image: busybox:1.36
      command:
        - sh
        - -c
        - |
          set -eux
          ls -R /data
          sleep 3600
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: %s
`, podName, namespace, pvcName)
}
