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
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("StatefulSet PVC manual recreate conversion", func() {
	It("[MTA-902] Convert a StatefulSet's PVC via volumeClaimTemplate rename + manual recreate", Label("tier0", "pvc-transfer"), func() {
		const (
			appName            = "cassandra"
			sourcePVCName      = "cassandra-data-cassandra-0"
			tempPVCName        = "cassandra-data-cassandra-0-cmk"
			fallbackDestSCName = "crane-dest-mta-902"
		)

		namespace := "mta-902"

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

		By("Grant namespace admin permissions to the nonadmin user")
		kubectl, cleanupNamespaceAdmin, err := SetupActiveNamespaceAdmin(scenario.KubectlSrc, scenario.KubectlSrcNonAdmin.Context, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanupNamespaceAdmin)

		paths, err := NewScenarioPaths("crane-mta-902-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}

		var destSCName string
		var cleanupDestSC func() error
		DeferCleanup(func() {
			By("Remove temp dir")
			if err := os.RemoveAll(paths.TempDir); err != nil {
				log.Printf("cleanup: %v", err)
			}
			By("Delete test namespace")
			if _, err := scenario.KubectlSrc.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=120s"); err != nil {
				log.Printf("cleanup: %v", err)
			}
			By("Cleanup destination StorageClass if this test created one")
			if cleanupDestSC != nil {
				if err := cleanupDestSC(); err != nil {
					log.Printf("cleanup: %v", err)
				}
			}
		})

		By("Deploy and validate source Cassandra")
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

		By("Resolve source StorageClass and choose a distinct destination class")
		srcPVC, err := GetPVC(srcApp.Context, namespace, sourcePVCName)
		Expect(err).NotTo(HaveOccurred())
		srcSC, err := ResolvePVCStorageClass(scenario.SrcApp.Context, *srcPVC)
		Expect(err).NotTo(HaveOccurred())
		Expect(srcSC).NotTo(BeEmpty(), "source PVC %s/%s must have a StorageClass", namespace, sourcePVCName)

		destSCName, cleanupDestSC, err = PrepareDestinationStorageClass(scenario.SrcApp.Context, srcSC, fallbackDestSCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(destSCName).NotTo(Equal(srcSC))

		By("Render crane output for the migrated namespace")
		runner.WorkDir = paths.TempDir
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		Expect(utils.AssertNoKindsInOutput(paths.OutputDir, []string{"PersistentVolumeClaim"})).NotTo(HaveOccurred())

		nodeIP, err := GetClusterNodeIP(scenario.SrcApp.Context)
		Expect(err).NotTo(HaveOccurred())

		stageTransfer := TransferPVCOptions{
			SourceContext:    srcApp.Context,
			TargetContext:    srcApp.Context,
			PVCName:          sourcePVCName + ":" + tempPVCName,
			PVCNamespaceMap:  fmt.Sprintf("%s:%s", namespace, namespace),
			DestStorageClass: destSCName,
			Subdomain:        fmt.Sprintf("%s.%s.%s.nip.io", tempPVCName, namespace, nodeIP),
		}

		By("Transfer the source PVC to a temporary same-namespace PVC on the new StorageClass")
		Expect(runner.TransferPVC(stageTransfer)).NotTo(HaveOccurred())
		assertNoTransferPVCLeftoversForNames(kubectl, []string{namespace}, sourcePVCName, tempPVCName)

		tempPVC, err := GetPVC(srcApp.Context, namespace, tempPVCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(PVCStorageClassName(*tempPVC)).To(Equal(destSCName))
		Expect(tempPVC.Status.Phase).To(Equal(corev1.ClaimBound))

		By("Scale down the source StatefulSet and wait for quiesce")
		Expect(kubectl.ScaleStatefulSet(namespace, appName, 0)).NotTo(HaveOccurred())
		WaitForSourceQuiesce(kubectl, namespace, "app="+appName, appName)

		By("Re-run transfer-pvc to capture final source writes into the temporary PVC")
		Expect(runner.TransferPVC(stageTransfer)).NotTo(HaveOccurred())
		assertNoTransferPVCLeftoversForNames(kubectl, []string{namespace}, sourcePVCName, tempPVCName)

		By("Delete the StatefulSet while orphaning the temporary PVCs")
		_, err = kubectl.Run("delete", "statefulset", appName, "-n", namespace, "--cascade=orphan", "--wait=true", "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		By("Delete the original PVC so the name can be recreated on the new StorageClass")
		_, err = kubectl.Run("delete", "pvc", sourcePVCName, "-n", namespace, "--ignore-not-found=false", "--wait=true", "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		By("Create a new empty PVC with the original StatefulSet-required name")
		Expect(applyPVCFromTemplate(kubectl, namespace, appName, sourcePVCName, destSCName, *srcPVC)).NotTo(HaveOccurred())

		finalTransfer := TransferPVCOptions{
			SourceContext:   srcApp.Context,
			TargetContext:   srcApp.Context,
			PVCName:         tempPVCName + ":" + sourcePVCName,
			PVCNamespaceMap: fmt.Sprintf("%s:%s", namespace, namespace),
			Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", sourcePVCName, namespace, nodeIP),
		}

		By("Copy data from the temporary PVC into the recreated original PVC name")
		Expect(runner.TransferPVC(finalTransfer)).NotTo(HaveOccurred())
		assertNoTransferPVCLeftoversForNames(kubectl, []string{namespace}, sourcePVCName, tempPVCName)

		By("Delete the temporary PVC after the final copy")
		_, err = kubectl.Run("delete", "pvc", tempPVCName, "-n", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		By("Re-apply the StatefulSet manifests in the same namespace")
		Expect(ApplyOutputToTargetNonAdmin(kubectl, paths.OutputDir)).NotTo(HaveOccurred())

		By("Validate Cassandra comes up on the recreated PVC")
		Eventually(srcApp.Validate, "10m", "10s").Should(Succeed())

		finalPVC, err := GetPVC(srcApp.Context, namespace, sourcePVCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(PVCStorageClassName(*finalPVC)).To(Equal(destSCName))
		Expect(finalPVC.Status.Phase).To(Equal(corev1.ClaimBound))

		By("Confirm no leftover transfer-pvc resources remain")
		assertNoTransferPVCLeftoversForNames(kubectl, []string{namespace}, sourcePVCName, tempPVCName)
	})
})

func assertNoTransferPVCLeftoversForNames(k KubectlRunner, namespaces []string, pvcNames ...string) {
	for _, ns := range namespaces {
		for _, pvcName := range pvcNames {
			selector := "app.konveyor.io/created-for-pvc=" + pvcName
			Eventually(func() (string, error) {
				out, err := k.GetResourceNamesByLabel(ns, selector)
				return strings.TrimSpace(out), err
			}, "2m", "5s").Should(BeEmpty(),
				"expected no leftover transfer-pvc resources in namespace %s for pvc label %s", ns, pvcName)
		}
	}
}

func applyPVCFromTemplate(k KubectlRunner, namespace, appName, pvcName, storageClass string, template corev1.PersistentVolumeClaim) error {
	if len(template.Spec.AccessModes) == 0 {
		return fmt.Errorf("template PVC %s/%s has no accessModes", template.Namespace, template.Name)
	}
	storageRequest, ok := template.Spec.Resources.Requests[corev1.ResourceStorage]
	if !ok {
		return fmt.Errorf("template PVC %s/%s has no storage request", template.Namespace, template.Name)
	}

	storageClassName := storageClass
	pvc := corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "PersistentVolumeClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespace,
			Labels: map[string]string{
				"app": appName,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: append([]corev1.PersistentVolumeAccessMode(nil), template.Spec.AccessModes...),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageRequest,
				},
			},
			StorageClassName: &storageClassName,
			VolumeMode:       template.Spec.VolumeMode,
		},
	}

	manifest, err := yaml.Marshal(pvc)
	if err != nil {
		return fmt.Errorf("marshal pvc manifest %s/%s: %w", namespace, pvcName, err)
	}
	if err := k.ApplyYAMLSpec(string(manifest), namespace); err != nil {
		return fmt.Errorf("apply pvc manifest %s/%s: %w", namespace, pvcName, err)
	}
	return nil
}
