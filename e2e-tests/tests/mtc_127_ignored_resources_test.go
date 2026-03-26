package e2e

import (
	"fmt"
	"log"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const manualEndpointsSpecTemplate = `apiVersion: v1
kind: Endpoints
metadata:
  name: mtc-127-manual-endpoint
  namespace: %s
subsets:
  - addresses:
      - ip: 10.0.0.123
    ports:
      - name: http
        port: 80
        protocol: TCP
`

const manualSubscriptionSpecTemplate = `apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: mtc-127-manual-subscription
  namespace: %s
spec:
  channel: stable
  installPlanApproval: Automatic
  name: packageserver
  source: operatorhubio-catalog
  sourceNamespace: olm
`

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
		manualEndpointsSpec := fmt.Sprintf(manualEndpointsSpecTemplate, namespace)
		By("Create manual Endpoints resource from inline YAML")
		Expect(kubectlSrc.ApplyYAMLSpec(manualEndpointsSpec)).NotTo(HaveOccurred())
		output, err := kubectlSrc.Run(fmt.Sprintf("get endpoints %s -n %s", "mtc-127-manual-endpoint", namespace))
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Endpoints resource is present on source cluster : %s\n", output)

		// Create subscription resource
		manualSubscriptionSpec := fmt.Sprintf(manualSubscriptionSpecTemplate, namespace)
		By("Create manual Subscription resource")
		Expect(kubectlSrc.ApplyYAMLSpec(manualSubscriptionSpec)).NotTo(HaveOccurred())
		out, err := kubectlSrc.Run("get", "subscription", "mtc-127-manual-subscription", "-n", namespace)
		Expect(err).NotTo(HaveOccurred(), "Subscription resource is not present on source cluster")
		log.Printf("Subscription resource is present on source cluster : %s\n", out)

		runner := scenario.Crane
		runner.WorkDir = paths.TempDir
		DeferCleanup(func() {
			By("Cleanup manual Endpoints resource on source")
			_, err := kubectlSrc.Run("delete", "endpoints", "mtc-127-manual-endpoint", "-n", namespace, "--ignore-not-found=true")
			if err != nil {
				log.Printf("cleanup manual endpoints failed: %v", err)
			}
			_, err = kubectlSrc.Run("delete", "subscription", "mtc-127-manual-subscription", "-n", namespace, "--ignore-not-found=true")
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

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		log.Printf("Validating resources created by app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)

		By("Validating manual resources on target cluster")
		log.Printf("Validating manual Endpoints resource on target cluster")
		output, err = kubectlTgt.Run("get", "endpoints", "mtc-127-manual-endpoint", "-n", tgtApp.Namespace)
		Expect(err).To(HaveOccurred(), "Endpoints resource is not present on target cluster")
		log.Printf("Endpoints resource is present on target cluster : %s\n", output)
		log.Printf("Validating manual Subscription resource on target cluster")
		output, err = kubectlTgt.Run("get", "subscription", "mtc-127-manual-subscription", "-n", namespace)
		Expect(err).To(HaveOccurred(), "Subscription resource is not present on target cluster")
		log.Printf("Subscription resource is present on target cluster : %s\n", output)

	})
})
