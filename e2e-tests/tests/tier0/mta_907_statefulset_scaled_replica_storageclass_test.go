package e2e

import (
	"fmt"
	"log"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("StatefulSet scaled replica StorageClass behavior", func() {
	It("[MTA-907] keeps the original StorageClass for a newly created StatefulSet replica", Label("tier0", "pvc-transfer"), func() {
		const (
			appName            = "cassandra"
			namespace          = "mta-907-cassandra"
			sourcePVCName      = "cassandra-data-cassandra-0"
			temporaryPVCName   = "cassandra-data-cassandra-0-mta-907"
			fallbackDestSCName = "crane-dest-mta-907"
		)

		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.SourceContext,
		)
		srcApp := scenario.SrcAppNonAdmin
		runner := scenario.CraneNonAdmin
		srcApp.ExtraVars = map[string]any{
			"non_admin_user":     "true",
			"number_of_replicas": 1,
		}

		kubectl, cleanupNamespaceAdmin, err := SetupActiveNamespaceAdmin(
			scenario.KubectlSrc,
			scenario.KubectlSrcNonAdmin.Context,
			namespace,
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanupNamespaceAdmin)

		paths, err := NewScenarioPaths("crane-mta-907-*")
		Expect(err).NotTo(HaveOccurred())
		runner.WorkDir = paths.TempDir

		var cleanupDestSC func() error
		DeferCleanup(func() {
			By("Clean up Cassandra, namespace, temporary files, and destination StorageClass")
			if err := srcApp.Cleanup(); err != nil {
				log.Printf("cleanup: failed to remove Cassandra: %v", err)
			}
			if _, err := scenario.KubectlSrc.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=120s"); err != nil {
				log.Printf("cleanup: failed to delete namespace %q: %v", namespace, err)
			}
			if err := os.RemoveAll(paths.TempDir); err != nil {
				log.Printf("cleanup: failed to remove temporary directory %q: %v", paths.TempDir, err)
			}
			if cleanupDestSC != nil {
				if err := cleanupDestSC(); err != nil {
					log.Printf("cleanup: failed to remove destination StorageClass: %v", err)
				}
			}
		})

		By("Deploy and validate one-replica Cassandra")
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

		By("Resolve the original StorageClass and prepare a distinct destination class")
		sourcePVC, err := GetPVC(srcApp.Context, namespace, sourcePVCName)
		Expect(err).NotTo(HaveOccurred())
		sourceSC, err := ResolvePVCStorageClass(scenario.SrcApp.Context, *sourcePVC)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceSC).NotTo(BeEmpty(), "source PVC %s/%s must have a StorageClass", namespace, sourcePVCName)
		originalTemplateSC, err := kubectl.Run("get", "statefulset", appName, "-n", namespace, "-o", "jsonpath={.spec.volumeClaimTemplates[0].spec.storageClassName}")
		Expect(err).NotTo(HaveOccurred())
		originalTemplateSC = strings.TrimSpace(originalTemplateSC)

		destinationSC, cleanupDestSC, err := PrepareDestinationStorageClass(scenario.SrcApp.Context, sourceSC, fallbackDestSCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(destinationSC).NotTo(Equal(sourceSC), "destination StorageClass must differ from the original class")

		nodeIP, err := GetClusterNodeIP(scenario.SrcApp.Context)
		Expect(err).NotTo(HaveOccurred())

		By("Scale Cassandra down before transferring its RWO PVC")
		Expect(kubectl.ScaleStatefulSet(namespace, appName, 0)).NotTo(HaveOccurred())
		WaitForSourceQuiesce(kubectl, namespace, "app="+appName, appName)

		stageTransfer := TransferPVCOptions{
			SourceContext:    srcApp.Context,
			TargetContext:    srcApp.Context,
			PVCName:          sourcePVCName + ":" + temporaryPVCName,
			PVCNamespaceMap:  fmt.Sprintf("%s:%s", namespace, namespace),
			DestStorageClass: destinationSC,
			Subdomain:        fmt.Sprintf("%s.%s.%s.nip.io", temporaryPVCName, namespace, nodeIP),
		}

		By("Transfer ordinal zero to a temporary PVC on the destination StorageClass")
		Expect(runner.TransferPVC(stageTransfer)).NotTo(HaveOccurred())
		AssertNoTransferPVCLeftovers(kubectl, []string{namespace}, sourcePVCName, temporaryPVCName)

		temporaryPVC, err := GetPVC(srcApp.Context, namespace, temporaryPVCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(PVCStorageClassName(*temporaryPVC)).To(Equal(destinationSC))
		Expect(temporaryPVC.Status.Phase).To(Equal(corev1.ClaimBound))

		By("Recreate ordinal zero under its original name on the destination StorageClass")
		_, err = kubectl.Run("delete", "pvc", sourcePVCName, "-n", namespace, "--wait=true", "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())
		Expect(recreatePVCWithStorageClass(kubectl, namespace, sourcePVCName, destinationSC, *sourcePVC)).NotTo(HaveOccurred())

		finalTransfer := TransferPVCOptions{
			SourceContext:   srcApp.Context,
			TargetContext:   srcApp.Context,
			PVCName:         temporaryPVCName + ":" + sourcePVCName,
			PVCNamespaceMap: fmt.Sprintf("%s:%s", namespace, namespace),
			Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", sourcePVCName, namespace, nodeIP),
		}

		By("Copy the migrated data back into ordinal zero")
		Expect(runner.TransferPVC(finalTransfer)).NotTo(HaveOccurred())
		AssertNoTransferPVCLeftovers(kubectl, []string{namespace}, sourcePVCName, temporaryPVCName)
		_, err = kubectl.Run("delete", "pvc", temporaryPVCName, "-n", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		finalPVC, err := GetPVC(srcApp.Context, namespace, sourcePVCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(PVCStorageClassName(*finalPVC)).To(Equal(destinationSC), "migrated ordinal zero must use the destination StorageClass")
		Expect(finalPVC.Status.Phase).To(Equal(corev1.ClaimBound))

		By("Scale the unchanged StatefulSet to two replicas")
		Expect(kubectl.ScaleStatefulSet(namespace, appName, 2)).NotTo(HaveOccurred())
		Eventually(func() error {
			out, err := kubectl.Run("get", "pvc", "-n", namespace, "-o", "jsonpath={.items[*].metadata.name}")
			if err != nil {
				return err
			}
			if !strings.Contains(out, "cassandra-data-cassandra-1") {
				return fmt.Errorf("ordinal one PVC has not been created yet")
			}
			return nil
		}, "5m", "10s").Should(Succeed())

		newReplicaPVC, err := GetPVC(srcApp.Context, namespace, "cassandra-data-cassandra-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(PVCStorageClassName(*newReplicaPVC)).To(Equal(sourceSC),
			"new StatefulSet replicas must retain the original StorageClass")
		Eventually(func() (corev1.PersistentVolumeClaimPhase, error) {
			pvc, err := GetPVC(srcApp.Context, namespace, "cassandra-data-cassandra-1")
			if err != nil {
				return "", err
			}
			return pvc.Status.Phase, nil
		}, "10m", "10s").Should(Equal(corev1.ClaimBound))
		currentTemplateSC, err := kubectl.Run("get", "statefulset", appName, "-n", namespace, "-o", "jsonpath={.spec.volumeClaimTemplates[0].spec.storageClassName}")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(currentTemplateSC)).To(Equal(originalTemplateSC),
			"the StatefulSet volumeClaimTemplates must remain unchanged during PVC conversion")

		By("Wait for both Cassandra pods to become ready")
		_, err = kubectl.Run(
			"wait", "pod", "cassandra-0", "cassandra-1",
			"-n", namespace,
			"--for=condition=Ready",
			"--timeout=10m",
		)
		Expect(err).NotTo(HaveOccurred())
		AssertNoTransferPVCLeftovers(kubectl, []string{namespace}, sourcePVCName)
	})
})

func recreatePVCWithStorageClass(k KubectlRunner, namespace, pvcName, storageClass string, template corev1.PersistentVolumeClaim) error {
	if len(template.Spec.AccessModes) == 0 {
		return fmt.Errorf("template PVC %s/%s has no accessModes", template.Namespace, template.Name)
	}
	storageRequest, ok := template.Spec.Resources.Requests[corev1.ResourceStorage]
	if !ok {
		return fmt.Errorf("template PVC %s/%s has no storage request", template.Namespace, template.Name)
	}

	storageClassName := storageClass
	pvc := corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespace,
			Labels:    map[string]string{"app": "cassandra"},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: append([]corev1.PersistentVolumeAccessMode(nil), template.Spec.AccessModes...),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: storageRequest},
			},
			StorageClassName: &storageClassName,
			VolumeMode:       template.Spec.VolumeMode,
		},
	}

	manifest, err := yaml.Marshal(pvc)
	if err != nil {
		return fmt.Errorf("marshal PVC manifest %s/%s: %w", namespace, pvcName, err)
	}
	if err := k.ApplyYAMLSpec(string(manifest), namespace); err != nil {
		return fmt.Errorf("apply PVC manifest %s/%s: %w", namespace, pvcName, err)
	}
	return nil
}
