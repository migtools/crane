package e2e

import (
	"fmt"
	"log"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("transfer-pvc progress output", func() {
	It("[MTA-817] reports numbered phases and a final summary for direct transfer",
		Label("tier1", "pvc-transfer", "direct"), func() {
			runTransferPVCProgressScenario(false)
		})

	It("[MTA-817] reports numbered phases and a final summary for indirect transfer",
		Label("tier1", "pvc-transfer", "indirect"), func() {
			if config.CloudStorage == "" {
				Skip("indirect transfer coverage requires --cloud-storage")
			}
			if config.RcloneConfigFile == "" && config.RcloneConfigSecret == "" {
				Skip("indirect transfer coverage requires an rclone config file or Secret")
			}
			runTransferPVCProgressScenario(true)
		})
})

// runTransferPVCProgressScenario uses the existing lightweight unattached-PVC
// app and seed-pod utilities. It isolates this test to transfer-pvc output;
// data-integrity and full migration workflow are already covered elsewhere.
func runTransferPVCProgressScenario(indirect bool) {
	mode := "direct"
	if indirect {
		mode = "indirect"
	}
	namespace := "mta-817-progress-" + mode
	const (
		appName     = "pvc"
		pvcName     = "data-pvc"
		seedPodName = "mta-817-seed"
	)

	scenario := NewMigrationScenario(
		appName, namespace, config.K8sDeployBin, config.CraneBin,
		config.SourceContext, config.TargetContext,
	)
	srcApp := scenario.SrcAppNonAdmin
	tgtApp := scenario.TgtAppNonAdmin
	srcApp.ExtraVars = map[string]any{
		"non_admin_user": "true",
		"pvc_name":       pvcName,
	}

	kubectlSrc, _, rbacCleanup, err := SetupActiveKubectlRunners(scenario, namespace)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(rbacCleanup)
	DeferCleanup(func() {
		for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
			if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
				log.Printf("cleanup namespace %q on context %q: %v", namespace, k.Context, err)
			}
		}
	})

	paths, err := NewScenarioPaths("crane-mta-817-" + mode + "-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
			log.Printf("cleanup apps/tempdir: %v", err)
		}
	})

	By("Deploy and seed an unattached source PVC")
	Expect(srcApp.Deploy()).NotTo(HaveOccurred())
	Expect(kubectlSrc.ApplyYAMLSpec(SeedPodManifest(namespace, seedPodName, pvcName), namespace)).NotTo(HaveOccurred())
	_, err = kubectlSrc.Run("wait", "--for=condition=Ready", "pod/"+seedPodName, "-n", namespace, "--timeout=120s")
	Expect(err).NotTo(HaveOccurred())
	_, err = kubectlSrc.Run("delete", "pod", seedPodName, "-n", namespace, "--wait=true")
	Expect(err).NotTo(HaveOccurred())

	options := TransferPVCOptions{
		SourceContext:       srcApp.Context,
		TargetContext:       tgtApp.Context,
		PVCName:             pvcName,
		PVCNamespaceMap:     fmt.Sprintf("%s:%s", namespace, namespace),
		DisableCloudStorage: !indirect,
	}
	var totalPhases int
	var phaseNames []string
	if indirect {
		options.CloudStorage = config.CloudStorage
		options.RcloneConfigFile = config.RcloneConfigFile
		options.RcloneConfigSecret = config.RcloneConfigSecret
		totalPhases = 6
		phaseNames = []string{
			"Reading source PVC", "Creating destination PVC", "Uploading data to cloud storage",
			"Downloading data from cloud storage", "Cleaning up cloud storage", "Cleaning up transfer pods",
		}
	} else {
		targetIP, err := GetClusterNodeIP(tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		options.Subdomain = fmt.Sprintf("%s.%s.%s.nip.io", pvcName, namespace, targetIP)
		totalPhases = 7
		phaseNames = []string{
			"Reading source PVC", "Creating destination PVC", "Creating endpoint",
			"Waiting for endpoint healthy", "Setting up secure tunnel and server",
			"Copying data (rsync)", "Cleaning up temporary resources",
		}
	}

	By("Transfer the PVC and verify the CLI progress ladder and final summary")
	runner := scenario.CraneNonAdmin
	runner.WorkDir = paths.TempDir
	output, err := runner.TransferPVCWithOutput(options)
	Expect(err).NotTo(HaveOccurred())
	Expect(AssertTransferPVCProgressOutput(output, totalPhases, phaseNames, "PVC data copy: succeeded")).To(Succeed())
}
