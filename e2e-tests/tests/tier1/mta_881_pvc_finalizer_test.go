package e2e

import (
	"fmt"
	"log"
	"os"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

var _ = Describe("PVC transfer with a stuck destination PVC", func() {
	It("[MTA-881] should fail clearly for a terminating target PVC and succeed after finalizer removal", Label("tier1", "pvc-transfer"), func() {
		namespace := "mta-881-terminating-pvc"
		pvcName := "data-pvc"
		seedPodName := "seed-pvc"
		verifyPodName := "verify-pvc"
		storageSize := "1Gi"
		finalizer := "crane.io/mta-881-test"

		scenario := NewMigrationScenario(
			"mta-881-pvc",
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)

		kubectlSrcNonAdmin, kubectlTgtNonAdmin, rbacCleanup, err := SetupActiveKubectlRunners(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(rbacCleanup)

		paths, err := NewScenarioPaths("crane-mta-881-*")
		Expect(err).NotTo(HaveOccurred())
		craneRunner := scenario.CraneNonAdmin
		craneRunner.WorkDir = paths.TempDir
		DeferCleanup(func() {
			if err := os.RemoveAll(paths.TempDir); err != nil {
				log.Printf("cleanup tempdir %q: %v", paths.TempDir, err)
			}
		})

		// This cleanup runs before namespace deletion, including when the test
		// fails while the target PVC is still protected by the finalizer.
		DeferCleanup(func() {
			if _, err := scenario.KubectlTgt.Run("delete", "pod", verifyPodName, "-n", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
				log.Printf("cleanup verifier pod %s/%s: %v", namespace, verifyPodName, err)
			}
			if _, err := scenario.KubectlTgt.Run("get", "pvc", pvcName, "-n", namespace); err == nil {
				if _, err := scenario.KubectlTgt.Run("patch", "pvc", pvcName, "-n", namespace, "--type=json", "-p", `[{"op":"remove","path":"/metadata/finalizers"}]`); err != nil {
					log.Printf("cleanup finalizer from PVC %s/%s: %v", namespace, pvcName, err)
				}
			}
		})

		By("Create and verify the source PVC")
		sourceStorageClass, err := DefaultStorageClassName(scenario.KubectlSrc.Context)
		Expect(err).NotTo(HaveOccurred())
		sourceManifest := pvcManifest(namespace, pvcName, sourceStorageClass, storageSize, "")
		Expect(kubectlSrcNonAdmin.ApplyYAMLSpec(sourceManifest, namespace)).NotTo(HaveOccurred())
		Eventually(func() (string, error) {
			return pvcPhase(kubectlSrcNonAdmin, namespace, pvcName)
		}, "2m", "5s").Should(Equal("Bound"), "source PVC %s/%s must be Bound", namespace, pvcName)

		By("Seed and verify known data on the source PVC")
		Expect(kubectlSrcNonAdmin.ApplyYAMLSpec(SeedPodManifest(namespace, seedPodName, pvcName), namespace)).NotTo(HaveOccurred())
		_, err = kubectlSrcNonAdmin.Run("wait", "--for=condition=Ready", "pod/"+seedPodName, "-n", namespace, "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())
		sourceData, err := ReadFileFromPod(kubectlSrcNonAdmin, namespace, seedPodName, "/data/hello.txt")
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceData).To(Equal("hello-from-source"), "source PVC seed data must be present before transfer")
		_, err = kubectlSrcNonAdmin.Run("delete", "pod", seedPodName, "-n", namespace, "--wait=true")
		Expect(err).NotTo(HaveOccurred())

		By("Create and delete a target PVC with a blocking finalizer")
		targetStorageClass, err := DefaultStorageClassName(scenario.KubectlTgt.Context)
		Expect(err).NotTo(HaveOccurred())
		blockerManifest := pvcManifest(namespace, pvcName, targetStorageClass, storageSize, finalizer)
		Expect(scenario.KubectlTgt.ApplyYAMLSpec(blockerManifest, namespace)).NotTo(HaveOccurred())
		_, err = scenario.KubectlTgt.Run("delete", "pvc", pvcName, "-n", namespace, "--wait=false")
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() (bool, error) {
			return pvcIsTerminating(scenario.KubectlTgt, namespace, pvcName)
		}, "30s", "2s").Should(BeTrue(), "target PVC %s/%s must enter Terminating state", namespace, pvcName)

		targetIP, err := GetClusterNodeIP(scenario.KubectlTgt.Context)
		Expect(err).NotTo(HaveOccurred())
		transferOpts := TransferPVCOptions{
			SourceContext:    scenario.SrcAppNonAdmin.Context,
			TargetContext:    scenario.TgtAppNonAdmin.Context,
			PVCName:          pvcName,
			PVCNamespaceMap:  fmt.Sprintf("%s:%s", namespace, namespace),
			DestStorageClass: targetStorageClass,
			Subdomain:        fmt.Sprintf("%s.%s.%s.nip.io", pvcName, namespace, targetIP),
		}

		By("Reject the transfer with an actionable Terminating PVC error")
		transferErr := craneRunner.TransferPVC(transferOpts)
		Expect(transferErr).To(HaveOccurred())
		expectedTerminatingError := fmt.Sprintf("destination PVC %q is terminating; transfer cannot proceed until it has been fully deleted. Remove the finalizer blocking deletion", namespace+"/"+pvcName)
		Expect(transferErr.Error()).To(ContainSubstring(expectedTerminatingError))
		AssertNoTransferPVCLeftovers(kubectlTgtNonAdmin, []string{namespace}, pvcName)

		By("Remove the finalizer and wait for the target PVC to finish deleting")
		_, err = scenario.KubectlTgt.Run("patch", "pvc", pvcName, "-n", namespace, "--type=json", "-p", `[{"op":"remove","path":"/metadata/finalizers"}]`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() (bool, error) {
			return pvcDeleted(scenario.KubectlTgt, namespace, pvcName)
		}, "2m", "2s").Should(BeTrue(), "target PVC %s/%s must be fully deleted before retry", namespace, pvcName)

		By("Retry the transfer after the naming conflict is cleared")
		Expect(craneRunner.TransferPVC(transferOpts)).NotTo(HaveOccurred())
		AssertNoTransferPVCLeftovers(kubectlTgtNonAdmin, []string{namespace}, pvcName)

		By("Verify the destination PVC is bound and contains the source data")
		Eventually(func() (string, error) {
			return pvcPhase(kubectlTgtNonAdmin, namespace, pvcName)
		}, "2m", "5s").Should(Equal("Bound"), "destination PVC %s/%s must be Bound", namespace, pvcName)
		targetPVC, err := GetPVC(kubectlTgtNonAdmin.Context, namespace, pvcName)
		Expect(err).NotTo(HaveOccurred())
		Expect(PVCStorageClassName(*targetPVC)).To(Equal(targetStorageClass), "destination PVC %s/%s must use the selected target StorageClass", namespace, pvcName)
		Expect(kubectlTgtNonAdmin.ApplyYAMLSpec(VerifyPodManifest(namespace, verifyPodName, pvcName), namespace)).NotTo(HaveOccurred())
		_, err = kubectlTgtNonAdmin.Run("wait", "--for=condition=Ready", "pod/"+verifyPodName, "-n", namespace, "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())
		targetData, err := ReadFileFromPod(kubectlTgtNonAdmin, namespace, verifyPodName, "/data/hello.txt")
		Expect(err).NotTo(HaveOccurred())
		Expect(targetData).To(Equal(sourceData), "destination PVC data must match the source after retry")
	})
})

func pvcManifest(namespace, name, storageClass, storageSize, finalizer string) string {
	finalizerBlock := ""
	if finalizer != "" {
		finalizerBlock = fmt.Sprintf("  finalizers:\n    - %s\n", finalizer)
	}
	return fmt.Sprintf(`apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %s
  namespace: %s
%sspec:
  storageClassName: %s
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: %s
`, name, namespace, finalizerBlock, storageClass, storageSize)
}

func pvcPhase(k KubectlRunner, namespace, name string) (string, error) {
	pvc, err := GetPVC(k.Context, namespace, name)
	if err != nil {
		return "", err
	}
	return string(pvc.Status.Phase), nil
}

func pvcIsTerminating(k KubectlRunner, namespace, name string) (bool, error) {
	pvc, err := GetPVC(k.Context, namespace, name)
	if err != nil {
		return false, err
	}
	return pvc.DeletionTimestamp != nil, nil
}

func pvcDeleted(k KubectlRunner, namespace, name string) (bool, error) {
	_, err := GetPVC(k.Context, namespace, name)
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}
