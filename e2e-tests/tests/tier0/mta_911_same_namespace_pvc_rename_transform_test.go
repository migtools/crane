package e2e

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Same-namespace PVC rename transform", func() {
	It("[MTA-911] Remaps a Deployment PVC reference after same-cluster PVC rename and StorageClass conversion", Label("tier0", "pvc-transfer"), func() {
		const (
			appName               = "mongodb"
			sourcePVCName         = "mongodb-data"
			destinationPVCName    = "mongodb-data-new"
			namespace             = "mta-911-pvc-rename"
			fallbackDestSCName    = "crane-dest-mta-911"
			expectedDocumentCount = 4
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
			"non_admin_user": "true",
		}

		By("Grant namespace-admin permissions to the non-admin user")
		kubectl, cleanupNamespaceAdmin, err := SetupActiveNamespaceAdmin(
			scenario.KubectlSrc,
			scenario.KubectlSrcNonAdmin.Context,
			namespace,
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanupNamespaceAdmin)

		paths, err := NewScenarioPaths("crane-mta-911-*")
		Expect(err).NotTo(HaveOccurred())
		runner.WorkDir = paths.TempDir

		var cleanupDestSC func() error
		DeferCleanup(func() {
			By("Clean up MongoDB, namespace, temporary files, and destination StorageClass")
			if err := srcApp.Cleanup(); err != nil {
				log.Printf("cleanup: failed to remove source app: %v", err)
			}
			if _, err := scenario.KubectlSrc.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=120s"); err != nil {
				log.Printf("cleanup: failed to delete namespace %q: %v", namespace, err)
			}
			if err := os.RemoveAll(paths.TempDir); err != nil {
				log.Printf("cleanup: failed to remove temp directory %q: %v", paths.TempDir, err)
			}
			if cleanupDestSC != nil {
				if err := cleanupDestSC(); err != nil {
					log.Printf("cleanup: failed to remove destination StorageClass: %v", err)
				}
			}
		})

		By("Deploy MongoDB and seed known data")
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())
		srcPodName, err := GetPodNameByLabel(kubectl, namespace, "name="+appName)
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectl.Run(
			"exec", srcPodName, "-n", namespace, "--",
			"mongosh", "sampledb",
			"--eval", `db.test_db.insertMany([{"a":1,"b":2},{"c":3,"d":4}]); print("seeded:", db.test_db.countDocuments())`,
			"--quiet",
		)
		Expect(err).NotTo(HaveOccurred())
		sourceDocumentCount, err := MongoDocumentCount(kubectl, namespace, srcPodName)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceDocumentCount).To(Equal(expectedDocumentCount),
			"source MongoDB should contain the two deployer seed documents and two test documents")

		By("Resolve the source StorageClass and prepare a different destination StorageClass")
		sourcePVC, err := GetPVC(srcApp.Context, namespace, sourcePVCName)
		Expect(err).NotTo(HaveOccurred())
		sourceSC, err := ResolvePVCStorageClass(scenario.SrcApp.Context, *sourcePVC)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceSC).NotTo(BeEmpty())
		var destinationSC string
		destinationSC, cleanupDestSC, err = PrepareDestinationStorageClass(scenario.SrcApp.Context, sourceSC, fallbackDestSCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(destinationSC).NotTo(Equal(sourceSC))

		By("Scale down MongoDB before transferring its RWO PVC")
		Expect(kubectl.ScaleDeploymentIfPresent(namespace, appName, 0)).NotTo(HaveOccurred())
		Eventually(func() (string, error) {
			out, err := kubectl.Run("get", "pods", "-n", namespace, "-l", "name="+appName, "-o", "name")
			return strings.TrimSpace(StripKubectlWarnings(out)), err
		}, "2m", "5s").Should(BeEmpty())

		By("Transfer the PVC to the new name and StorageClass in the same namespace")
		nodeIP, err := GetClusterNodeIP(scenario.SrcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(runner.TransferPVC(TransferPVCOptions{
			SourceContext:    srcApp.Context,
			TargetContext:    srcApp.Context,
			PVCName:          fmt.Sprintf("%s:%s", sourcePVCName, destinationPVCName),
			PVCNamespaceMap:  fmt.Sprintf("%s:%s", namespace, namespace),
			DestStorageClass: destinationSC,
			Subdomain:        fmt.Sprintf("%s.nip.io", nodeIP),
		})).NotTo(HaveOccurred())
		AssertNoTransferPVCLeftovers(kubectl, []string{namespace}, sourcePVCName, destinationPVCName)

		By("Verify the renamed PVC exists with the converted StorageClass")
		_, err = GetPVC(srcApp.Context, namespace, sourcePVCName)
		Expect(err).NotTo(HaveOccurred())
		destinationPVC, err := GetPVC(srcApp.Context, namespace, destinationPVCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(PVCStorageClassName(*destinationPVC)).To(Equal(destinationSC))

		exportOpts := ExportOptions{Namespace: namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{
			ExportDir:    paths.ExportDir,
			TransformDir: paths.TransformDir,
			OptionalFlags: fmt.Sprintf(
				`{"pvc-rename-map":"%s:%s"}`,
				sourcePVCName,
				destinationPVCName,
			),
		}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}

		By("Export and transform without applying, then inspect the transformed Deployment")
		Expect(runner.Export(exportOpts)).NotTo(HaveOccurred())
		Expect(runner.Transform(transformOpts)).NotTo(HaveOccurred())
		deploymentManifest, err := transformedDeploymentManifest(paths.TransformDir, namespace)
		Expect(err).NotTo(HaveOccurred())
		manifest, err := os.ReadFile(deploymentManifest)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(manifest)).To(ContainSubstring("claimName: " + destinationPVCName))
		staleClaimName := regexp.MustCompile(`(?m)^\s*claimName:\s*` + regexp.QuoteMeta(sourcePVCName) + `\s*$`)
		Expect(string(manifest)).NotTo(MatchRegexp(staleClaimName.String()))

		By("Apply the transformed manifests in the same namespace")
		Expect(runner.Apply(applyOpts)).NotTo(HaveOccurred())
		VerifyPVCRenameInOutput(paths.OutputDir, destinationPVCName)
		Expect(ApplyOutputToTargetNonAdmin(kubectl, paths.OutputDir)).NotTo(HaveOccurred())

		By("Restart MongoDB and verify the migrated data")
		Expect(kubectl.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
		Eventually(func() (int, error) {
			targetPodName, err := GetPodNameByLabel(kubectl, namespace, "name="+appName)
			if err != nil {
				return 0, err
			}
			return MongoDocumentCount(kubectl, namespace, targetPodName)
		}, "5m", "10s").Should(Equal(sourceDocumentCount),
			"destination MongoDB should contain the same documents after mounting the renamed PVC")

		By("Verify the destination PVC remains Bound and helper resources are gone")
		destinationPVC, err = GetPVC(srcApp.Context, namespace, destinationPVCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(destinationPVC.Status.Phase).To(Equal(corev1.ClaimBound))
		AssertNoTransferPVCLeftovers(kubectl, []string{namespace}, sourcePVCName, destinationPVCName)
	})
})

func transformedDeploymentManifest(transformDir, namespace string) (string, error) {
	var deploymentPath string
	err := filepath.WalkDir(transformDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.Contains(path, string(filepath.Separator)+"output"+string(filepath.Separator)+namespace+string(filepath.Separator)) {
			return nil
		}
		if strings.HasPrefix(filepath.Base(path), "Deployment_") && strings.HasSuffix(path, ".yaml") {
			deploymentPath = path
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("search transformed Deployment in %q: %w", transformDir, err)
	}
	if deploymentPath == "" {
		files, listErr := utils.ListFilesRecursivelyAsList(transformDir)
		if listErr != nil {
			return "", fmt.Errorf("list transformed files in %q: %w", transformDir, listErr)
		}
		return "", fmt.Errorf("no transformed Deployment found for namespace %q in %q (files: %v)", namespace, transformDir, files)
	}
	return deploymentPath, nil
}
