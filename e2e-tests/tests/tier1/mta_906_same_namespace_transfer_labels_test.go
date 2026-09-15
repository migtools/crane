package e2e

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Same-namespace transfer-pvc resources", func() {
	It("[MTA-906] uses distinct TLS secrets and client/server role labels", Label("tier1", "pvc-transfer"), func() {
		if config.CloudStorage != "" {
			Skip("MTA-906 validates direct rsync/stunnel transfer resources")
		}

		const (
			appName            = "mongodb"
			namespace          = "mta-906"
			sourcePVCName      = "mongodb-data"
			destinationPVCName = "mongodb-data-new"
			destinationSCName  = "crane-dest-mta-906"
			clientRole         = "client"
			serverRole         = "server"
			roleLabel          = "app.konveyor.io/role"
			createdForPVCLabel = "app.konveyor.io/created-for-pvc"
			secretNamePrefix   = "stunnel-creds-certs-"
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
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin
		srcApp.ExtraVars = map[string]any{"non_admin_user": "true"}
		tgtApp.ExtraVars = map[string]any{"non_admin_user": "true"}

		paths, err := NewScenarioPaths("crane-mta-906-*")
		Expect(err).NotTo(HaveOccurred())
		runner.WorkDir = paths.TempDir

		By("Grant namespace-admin permissions to the non-admin user")
		kubectl, cleanupNamespaceAdmin, err := SetupActiveNamespaceAdmin(
			scenario.KubectlSrc,
			scenario.KubectlSrcNonAdmin.Context,
			namespace,
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanupNamespaceAdmin)

		var cleanupDestSC func() error
		DeferCleanup(func() {
			By("Clean up MongoDB, namespace, temporary files, and destination StorageClass")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
			if _, err := scenario.KubectlSrc.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=120s"); err != nil {
				log.Printf("cleanup: failed to delete namespace %q: %v", namespace, err)
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
		Expect(sourceDocumentCount).To(Equal(4), "source MongoDB should contain the deployer and test documents")

		By("Resolve the source StorageClass and prepare a different destination StorageClass")
		sourcePVC, err := GetPVC(srcApp.Context, namespace, sourcePVCName)
		Expect(err).NotTo(HaveOccurred())
		sourceSC, err := ResolvePVCStorageClass(scenario.SrcApp.Context, *sourcePVC)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceSC).NotTo(BeEmpty(), "source PVC must have a StorageClass")
		var destinationSC string
		destinationSC, cleanupDestSC, err = PrepareDestinationStorageClass(scenario.SrcApp.Context, sourceSC, destinationSCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(destinationSC).NotTo(Equal(sourceSC))

		By("Scale down MongoDB and wait for the PVC to be quiesced")
		Expect(kubectl.ScaleDeploymentIfPresent(namespace, appName, 0)).NotTo(HaveOccurred())
		Eventually(func() (string, error) {
			out, err := kubectl.Run("get", "pods", "-n", namespace, "-l", "name="+appName, "-o", "name")
			return StripKubectlWarnings(out), err
		}, "2m", "5s").Should(BeEmpty())

		By("Render the workload with the renamed PVC reference")
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
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		VerifyPVCRenameInOutput(paths.OutputDir, destinationPVCName)

		By("Run transfer-pvc and inspect transient client/server resources")
		nodeIP, err := GetClusterNodeIP(scenario.SrcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		transferOpts := TransferPVCOptions{
			SourceContext:    srcApp.Context,
			TargetContext:    srcApp.Context,
			PVCName:          fmt.Sprintf("%s:%s", sourcePVCName, destinationPVCName),
			PVCNamespaceMap:  fmt.Sprintf("%s:%s", namespace, namespace),
			DestStorageClass: destinationSC,
			Subdomain:        fmt.Sprintf("%s.nip.io", nodeIP),
		}
		transferDone := make(chan error, 1)
		go func() {
			transferDone <- runner.TransferPVC(transferOpts)
		}()

		clientPods, serverPods, clientSecrets, serverSecrets := waitForTransferResources(kubectl, namespace, sourcePVCName, destinationPVCName, createdForPVCLabel)
		Expect(<-transferDone).NotTo(HaveOccurred())

		By("Verify client/server labels and distinct TLS secret names")
		Expect(hasRole(clientPods, clientRole, roleLabel)).To(BeTrue(), "source client pod should have role=%s", clientRole)
		Expect(hasRole(serverPods, serverRole, roleLabel)).To(BeTrue(), "destination server pod should have role=%s", serverRole)
		Expect(hasRole(clientSecrets, clientRole, roleLabel)).To(BeTrue(), "source client secret should have role=%s", clientRole)
		Expect(hasRole(serverSecrets, serverRole, roleLabel)).To(BeTrue(), "destination server secret should have role=%s", serverRole)
		Expect(resourceNames(clientSecrets)).To(ConsistOf(secretNamePrefix + sourcePVCName))
		Expect(resourceNames(serverSecrets)).To(ConsistOf(secretNamePrefix + destinationPVCName))
		Expect(resourceNames(clientSecrets)[0]).NotTo(Equal(resourceNames(serverSecrets)[0]), "client and server certificate secrets must not collide")

		By("Verify the renamed destination PVC uses the destination StorageClass")
		destinationPVC, err := GetPVC(srcApp.Context, namespace, destinationPVCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(destinationPVC.Status.Phase).To(Equal(corev1.ClaimBound))
		Expect(PVCStorageClassName(*destinationPVC)).To(Equal(destinationSC))

		By("Confirm transfer helper resources are gone")
		AssertNoTransferPVCLeftovers(kubectl, []string{namespace}, sourcePVCName, destinationPVCName)

		By("Apply the renamed workload and verify the transferred data")
		Expect(applyTransformedDeployment(kubectl, paths.OutputDir, namespace)).NotTo(HaveOccurred())
		Expect(kubectl.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
		Eventually(func() (int, error) {
			targetPodName, err := GetPodNameByLabel(kubectl, namespace, "name="+appName)
			if err != nil {
				return 0, err
			}
			return MongoDocumentCount(kubectl, namespace, targetPodName)
		}, "5m", "10s").Should(Equal(sourceDocumentCount),
			"destination MongoDB should contain the same documents after the renamed same-namespace transfer")
	})
})

func applyTransformedDeployment(k KubectlRunner, outputDir, namespace string) error {
	manifests, err := filepath.Glob(filepath.Join(outputDir, "resources", namespace, "Deployment_*.yaml"))
	if err != nil {
		return fmt.Errorf("find transformed Deployment manifests: %w", err)
	}
	if len(manifests) != 1 {
		return fmt.Errorf("expected one transformed Deployment manifest in %q, found %d", filepath.Join(outputDir, "resources", namespace), len(manifests))
	}
	manifest, err := os.ReadFile(manifests[0])
	if err != nil {
		return fmt.Errorf("read transformed Deployment manifest %q: %w", manifests[0], err)
	}
	return k.ApplyYAMLSpec(string(manifest), namespace)
}

type labeledResource struct {
	Name   string
	Labels map[string]string
}

// waitForTransferResources waits until both same-namespace transfer sides expose their pods and TLS secrets.
func waitForTransferResources(k KubectlRunner, namespace, sourcePVCName, destinationPVCName, createdForPVCLabel string) ([]labeledResource, []labeledResource, []labeledResource, []labeledResource) {
	var clientPods, serverPods, clientSecrets, serverSecrets []labeledResource
	Eventually(func() error {
		var err error
		clientPods, err = getLabeledResources(k, namespace, "pods", createdForPVCLabel+"="+sourcePVCName)
		if err != nil {
			return err
		}
		serverPods, err = getLabeledResources(k, namespace, "pods", createdForPVCLabel+"="+destinationPVCName)
		if err != nil {
			return err
		}
		clientSecrets, err = getLabeledResources(k, namespace, "secrets", createdForPVCLabel+"="+sourcePVCName)
		if err != nil {
			return err
		}
		serverSecrets, err = getLabeledResources(k, namespace, "secrets", createdForPVCLabel+"="+destinationPVCName)
		if err != nil {
			return err
		}
		if len(clientPods) == 0 || len(serverPods) == 0 || len(clientSecrets) == 0 || len(serverSecrets) == 0 {
			return fmt.Errorf("waiting for same-namespace transfer resources: client pods=%d, server pods=%d, client secrets=%d, server secrets=%d", len(clientPods), len(serverPods), len(clientSecrets), len(serverSecrets))
		}
		return nil
	}, "3m", "500ms").Should(Succeed())
	return clientPods, serverPods, clientSecrets, serverSecrets
}

// getLabeledResources returns Kubernetes resources and their labels for a namespace-scoped selector.
func getLabeledResources(k KubectlRunner, namespace, kind, selector string) ([]labeledResource, error) {
	out, err := k.Run("get", kind, "-n", namespace, "-l", selector, "-o", "json")
	if err != nil {
		return nil, err
	}
	var list unstructured.UnstructuredList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("parse %s resources for selector %q: %w", kind, selector, err)
	}
	resources := make([]labeledResource, 0, len(list.Items))
	for _, item := range list.Items {
		accessor, err := meta.Accessor(&item)
		if err != nil {
			return nil, fmt.Errorf("read metadata for %s resource selected by %q: %w", kind, selector, err)
		}
		resources = append(resources, labeledResource{Name: accessor.GetName(), Labels: accessor.GetLabels()})
	}
	return resources, nil
}

// hasRole reports whether every selected resource has the expected transfer role label.
func hasRole(resources []labeledResource, role, roleLabel string) bool {
	if len(resources) == 0 {
		return false
	}
	for _, resource := range resources {
		if resource.Labels[roleLabel] != role {
			return false
		}
	}
	return true
}

// resourceNames extracts resource names for exact-name assertions.
func resourceNames(resources []labeledResource) []string {
	names := make([]string, 0, len(resources))
	for _, resource := range resources {
		names = append(names, resource.Name)
	}
	return names
}
