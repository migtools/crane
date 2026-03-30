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

// const manualEndpointsSpecTemplate = `apiVersion: v1
// kind: Endpoints
// metadata:
//   name: mtc-127-manual-endpoint
//   namespace: %s
// subsets:
//   - addresses:
//       - ip: 10.0.0.123
//     ports:
//       - name: http
//         port: 80
//         protocol: TCP
// `

// const manualSubscriptionSpecTemplate = `
// `

var _ = Describe("[MTC-127] Default Ignored resources", func() {
	It("should be ignored", Label("tier0"), func() {
		appName := "empty-namespace"
		namespace := appName
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		srcApp := scenario.SrcApp
		tgtApp := scenario.TgtApp
		kubectlSrc := scenario.KubectlSrc
		kubectlTgt := scenario.KubectlTgt
		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		manualEndpointsSpec, err := utils.ReadTestdataFile("endpoints.yaml")
		Expect(err).NotTo(HaveOccurred())
		By("Create manual Endpoints resource from inline YAML")
		Expect(kubectlSrc.ApplyYAMLSpec(manualEndpointsSpec, namespace)).NotTo(HaveOccurred())
		output, err := kubectlSrc.Run(fmt.Sprintf("get endpoints %s -n %s", "manual-endpoint", namespace))
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Endpoints resource is present on source cluster \n%s\n", output)

		// Create subscription resource
		manualSubscriptionSpec, err := utils.ReadTestdataFile("subscription.yaml")
		Expect(err).NotTo(HaveOccurred())
		By("Create manual Subscription resource")
		Expect(kubectlSrc.ApplyYAMLSpec(manualSubscriptionSpec, namespace)).NotTo(HaveOccurred())
		out, err := kubectlSrc.Run("get", "subscription", "manual-subscription", "-n", namespace)
		Expect(err).NotTo(HaveOccurred(), "Subscription resource is not present on source cluster")
		log.Printf("Subscription resource is present on source cluster\n %s\n", out)

		runner := scenario.Crane
		runner.WorkDir = paths.TempDir
		DeferCleanup(func() {
			By("Cleanup manual Endpoints resource on source")
			_, err := kubectlSrc.Run("delete", "endpoints", "manual-endpoint", "-n", namespace, "--ignore-not-found=true")
			if err != nil {
				log.Printf("cleanup manual endpoints failed: %v", err)
			}
			_, err = kubectlSrc.Run("delete", "subscription", "manual-subscription", "-n", namespace, "--ignore-not-found=true")
			if err != nil {
				log.Printf("cleanup manual subscription failed: %v", err)
			}
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})
		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, srcApp.Namespace, paths)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)
		By("Verify output directory does not contain ignored resource manifests")
		outputFiles, err := utils.ListFilesRecursivelyAsList(paths.OutputDir)
		Expect(err).NotTo(HaveOccurred())
		for _, f := range outputFiles {
			Expect(strings.Contains(f, "Endpoint")).To(BeFalse(), "output contains an Endpoints manifest: %s", f)
			Expect(strings.Contains(f, "Subscription")).To(BeFalse(), "output contains a Subscription manifest: %s", f)
		}
		log.Printf("Verified output dir does not include Endpoints/Subscription manifests")

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		log.Printf("Validating resources created by app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)

		By("Validating manual resources on target cluster")
		log.Printf("Verifying manual Endpoints resource is NOT present on target cluster")
		output, err = kubectlTgt.Run("get", "endpoints", "mtc-127-manual-endpoint", "-n", tgtApp.Namespace)
		Expect(err).To(HaveOccurred(), "Endpoints resource should NOT be present on target cluster but was found")
		log.Printf("Confirmed: Endpoints resource is correctly absent from target cluster\n")
		log.Printf("Verifying manual Subscription resource is NOT present on target cluster")
		output, err = kubectlTgt.Run("get", "subscription", "mtc-127-manual-subscription", "-n", namespace)
		Expect(err).To(HaveOccurred(), "Subscription resource should NOT be present on target cluster but was found")
		log.Printf("Confirmed: Subscription resource is correctly absent from target cluster\n")

	})
})
