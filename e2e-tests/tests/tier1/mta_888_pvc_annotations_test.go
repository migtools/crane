package e2e

import (
	"fmt"
	"log"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	mta888AnnotationKey   = "e2e.crane.konveyor.io/retention"
	mta888AnnotationValue = "keep-unchanged"
	mta888PVCName         = "mongodb-data"
)

var _ = Describe("PVC annotations during application migration", func() {
	It("[MTA-888] preserves user PVC annotations and lets the target provisioner set binding annotations",
		Label("tier1", "pvc-transfer"), func() {
			const (
				appName   = "mongodb"
				namespace = "mta-888-pvc-annotations"
			)

			scenario := NewMigrationScenario(
				appName, namespace, config.K8sDeployBin, config.CraneBin,
				config.SourceContext, config.TargetContext,
			)
			srcApp := scenario.SrcAppNonAdmin
			tgtApp := scenario.TgtAppNonAdmin
			runner := scenario.CraneNonAdmin
			srcApp.ExtraVars = map[string]any{
				"non_admin_user": "true",
				"has_scc":        scenario.KubectlSrc.IsOpenShift(),
			}
			tgtApp.ExtraVars = map[string]any{
				"non_admin_user": "true",
				"has_scc":        scenario.KubectlTgt.IsOpenShift(),
			}

			By("Grant namespace-admin permissions to the test user on source and target")
			kubectlSrc, kubectlTgt, rbacCleanup, err := SetupActiveKubectlRunners(scenario, namespace)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(rbacCleanup)
			DeferCleanup(func() {
				for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
					if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
						log.Printf("cleanup namespace %q on context %q: %v", namespace, k.Context, err)
					}
				}
			})

			paths, err := NewScenarioPaths("crane-mta-888-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
					log.Printf("cleanup apps/tempdir: %v", err)
				}
			})

			By("Deploy a source MongoDB application with a dynamically provisioned PVC")
			Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

			By("Record source binding metadata and add a user-defined PVC annotation")
			srcPVC, err := GetPVC(scenario.KubectlSrc.Context, namespace, mta888PVCName)
			Expect(err).NotTo(HaveOccurred())
			Expect(srcPVC.Status.Phase).To(Equal(corev1.ClaimBound))
			sourceProvisioner := srcPVC.Annotations["volume.kubernetes.io/storage-provisioner"]
			Expect(sourceProvisioner).NotTo(BeEmpty(), "source PVC should have a provisioner annotation")
			Expect(srcPVC.Annotations["pv.kubernetes.io/bind-completed"]).To(Equal("yes"),
				"source PVC should have binding-completed metadata")
			sourceStorageClass, err := ResolvePVCStorageClass(scenario.KubectlSrc.Context, *srcPVC)
			Expect(err).NotTo(HaveOccurred())
			log.Printf("Source PVC %s/%s: StorageClass=%s provisioner=%s", namespace, mta888PVCName, sourceStorageClass, sourceProvisioner)

			_, err = kubectlSrc.Run("annotate", "pvc", mta888PVCName, "-n", namespace,
				mta888AnnotationKey+"="+mta888AnnotationValue, "--overwrite")
			Expect(err).NotTo(HaveOccurred())
			srcPVC, err = GetPVC(scenario.KubectlSrc.Context, namespace, mta888PVCName)
			Expect(err).NotTo(HaveOccurred())
			Expect(srcPVC.Annotations).To(HaveKeyWithValue(mta888AnnotationKey, mta888AnnotationValue))

			By("Quiesce MongoDB and render its workload without the PVC manifest")
			Expect(kubectlSrc.ScaleDeploymentIfPresent(namespace, appName, 0)).NotTo(HaveOccurred())
			WaitForSourceQuiesce(kubectlSrc, namespace, "name="+appName, appName)
			runner.WorkDir = paths.TempDir
			Expect(RunCranePipelineWithChecks(runner,
				ExportOptions{Namespace: namespace, ExportDir: paths.ExportDir},
				TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir},
				ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir},
			)).NotTo(HaveOccurred())

			By("Transfer the PVC before the destination application is started")
			targetIP, err := GetClusterNodeIP(scenario.KubectlTgt.Context)
			Expect(err).NotTo(HaveOccurred())
			Expect(runner.TransferPVC(TransferPVCOptions{
				SourceContext:   srcApp.Context,
				TargetContext:   tgtApp.Context,
				PVCName:         mta888PVCName,
				PVCNamespaceMap: fmt.Sprintf("%s:%s", namespace, namespace),
				Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", mta888PVCName, namespace, targetIP),
			})).NotTo(HaveOccurred())

			By("Apply the workload, start MongoDB, and confirm the destination PVC binds")
			Expect(ApplyOutputToTargetNonAdmin(kubectlTgt, paths.OutputDir)).NotTo(HaveOccurred())
			Expect(kubectlTgt.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
			Eventually(tgtApp.Validate, "5m", "10s").Should(Succeed())

			destPVC, err := GetPVC(scenario.KubectlTgt.Context, namespace, mta888PVCName)
			Expect(err).NotTo(HaveOccurred())
			Expect(destPVC.Status.Phase).To(Equal(corev1.ClaimBound), "destination PVC must bind after the application starts")
			Expect(destPVC.Annotations).To(HaveKeyWithValue(mta888AnnotationKey, mta888AnnotationValue),
				"user-defined annotations must be preserved unchanged")

			By("Verify the destination PVC has provisioner metadata matching its StorageClass")
			destStorageClass, err := ResolvePVCStorageClass(scenario.KubectlTgt.Context, *destPVC)
			Expect(err).NotTo(HaveOccurred())
			provisioner, err := scenario.KubectlTgt.Run("get", "storageclass", destStorageClass, "-o", "jsonpath={.provisioner}")
			Expect(err).NotTo(HaveOccurred())
			provisioner = strings.TrimSpace(provisioner)
			Expect(provisioner).NotTo(BeEmpty(), "destination StorageClass %q must name a provisioner", destStorageClass)
			Expect(destPVC.Annotations["pv.kubernetes.io/bind-completed"]).To(Equal("yes"))
			Expect(destPVC.Annotations["volume.kubernetes.io/storage-provisioner"]).To(Equal(provisioner),
				"destination PVC must use metadata generated by its own provisioner")
			log.Printf("Destination PVC %s/%s: StorageClass=%s provisioner=%s", namespace, mta888PVCName, destStorageClass, provisioner)
		})
})
