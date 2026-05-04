package e2e

import (
	"log"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// olmWhiteoutKinds lists Kubernetes object kinds that crane-lib whiteouts for OLM migration.
var olmWhiteoutKinds = []string{
	"Subscription",
	"CatalogSource",
	"ClusterServiceVersion",
	"InstallPlan",
	"OperatorGroup",
	"OperatorCondition",
}

var _ = Describe("OLM whiteout", func() {
	Describe("Baseline full OLM graph", func() {
		It("should omit OLM kinds from crane apply output", Label("olm", "tier0"), func() {
			kubectlPreflight := KubectlRunner{Bin: "kubectl", Context: config.SourceContext}
			olmAvailable, err := kubectlPreflight.OLMAPIAvailable()
			Expect(err).NotTo(HaveOccurred())
			if !olmAvailable {
				Skip("OLM APIs not installed (subscriptions.operators.coreos.com CRD missing)")
			}

			appName := "olm-baseline"
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

			By("Prepare source app (olm-baseline k8sdeploy: namespace, RBAC, OperatorGroup, CatalogSource, Subscription)")
			log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
			Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())

			paths, err := NewScenarioPaths("crane-export-*")
			Expect(err).NotTo(HaveOccurred())

			DeferCleanup(func() {
				By("Cleanup source and target resources")
				if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
					log.Printf("cleanup: %v", err)
				}
			})

			By("Wait for OLM to create InstallPlan and ClusterServiceVersion")
			Eventually(func(g Gomega) {
				out, err := kubectlSrc.Run("get", "installplan", "-n", namespace)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(out)).NotTo(ContainSubstring("No resources found"))
			}, "12m", "15s").Should(Succeed())
			Eventually(func(g Gomega) {
				out, err := kubectlSrc.Run("get", "csv", "-n", namespace)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(out)).NotTo(ContainSubstring("No resources found"))
			}, "12m", "15s").Should(Succeed())

			runner := scenario.Crane
			runner.WorkDir = paths.TempDir

			By("Run crane export/transform/apply pipeline")
			log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
			Expect(RunCranePipelineWithChecks(runner, srcApp.Namespace, paths)).NotTo(HaveOccurred())
			log.Printf("Crane pipeline completed for namespace %s", srcApp.Namespace)

			By("Verify output directory does not contain OLM whiteout kinds")
			Expect(utils.AssertNoKindsInOutput(paths.OutputDir, olmWhiteoutKinds)).NotTo(HaveOccurred())

			By("Apply rendered manifests to target")
			Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

			By("Verify OLM objects from baseline setup are not present on target")
			for _, res := range []struct {
				kind string
				name string
			}{
				{"subscription", "olm-whiteout-subscription"},
				{"catalogsource", "olm-whiteout-catalog"},
				{"operatorgroup", "olm-whiteout-og"},
			} {
				_, err := kubectlTgt.Run("get", res.kind, res.name, "-n", namespace)
				Expect(err).To(HaveOccurred(), "%s %s should not exist on target", res.kind, res.name)
				Expect(err.Error()).To(ContainSubstring("NotFound"))
			}
		})
	})

	Describe("App workload coexistence", func() {
		It("should preserve app resources while omitting OLM kinds", Label("olm", "tier1"), func() {
			kubectlPreflight := KubectlRunner{Bin: "kubectl", Context: config.SourceContext}
			olmAvailable, err := kubectlPreflight.OLMAPIAvailable()
			Expect(err).NotTo(HaveOccurred())
			if !olmAvailable {
				Skip("OLM APIs not installed (subscriptions.operators.coreos.com CRD missing)")
			}

			appName := "simple-nginx-nopv"
			namespace := "olm-app-coexist"
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

			By("Deploy application workloads via k8sdeploy (Deployment + Service)")
			log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
			Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())

			paths, err := NewScenarioPaths("crane-export-*")
			Expect(err).NotTo(HaveOccurred())

			DeferCleanup(func() {
				By("Cleanup OLM resources on source")
				for _, res := range []struct {
					kind string
					name string
				}{
					{"subscription", "olm-whiteout-subscription"},
					{"catalogsource", "olm-whiteout-catalog"},
					{"operatorgroup", "olm-whiteout-og"},
				} {
					if _, err := kubectlSrc.Run("delete", res.kind, res.name, "-n", namespace, "--ignore-not-found=true"); err != nil {
						log.Printf("cleanup: failed to delete %s/%s: %v", res.kind, res.name, err)
					}
				}
				By("Cleanup source and target resources")
				if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
					log.Printf("cleanup: %v", err)
				}
			})

			By("Create ConfigMap as additional app resource")
			configMapSpec, err := utils.ReadTestdataFile("app-configmap.yaml")
			Expect(err).NotTo(HaveOccurred())
			Expect(kubectlSrc.ApplyYAMLSpec(configMapSpec, namespace)).NotTo(HaveOccurred())

			By("Create OLM resources (OperatorGroup, CatalogSource, Subscription)")
			olmSpec, err := utils.ReadTestdataFile("olm-resources.yaml")
			Expect(err).NotTo(HaveOccurred())
			olmSpec = strings.ReplaceAll(olmSpec, "__NAMESPACE__", namespace)
			Expect(kubectlSrc.ApplyYAMLSpec(olmSpec, namespace)).NotTo(HaveOccurred())

			By("Wait for OLM to create InstallPlan and ClusterServiceVersion")
			Eventually(func(g Gomega) {
				out, err := kubectlSrc.Run("get", "installplan", "-n", namespace)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(out)).NotTo(ContainSubstring("No resources found"))
			}, "12m", "15s").Should(Succeed())
			Eventually(func(g Gomega) {
				out, err := kubectlSrc.Run("get", "csv", "-n", namespace)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(out)).NotTo(ContainSubstring("No resources found"))
			}, "12m", "15s").Should(Succeed())

			runner := scenario.Crane
			runner.WorkDir = paths.TempDir

			By("Run crane export/transform/apply pipeline")
			log.Printf("Running crane pipeline for namespace %s\n", namespace)
			Expect(RunCranePipelineWithChecks(runner, namespace, paths)).NotTo(HaveOccurred())
			log.Printf("Crane pipeline completed for namespace %s", namespace)

			By("Verify output does not contain OLM whiteout kinds")
			Expect(utils.AssertNoKindsInOutput(paths.OutputDir, olmWhiteoutKinds)).NotTo(HaveOccurred())

			By("Verify output contains application resource kinds (Deployment, Service, ConfigMap)")
			Expect(utils.AssertKindsInOutput(paths.OutputDir, []string{"Deployment", "Service", "ConfigMap"})).NotTo(HaveOccurred())

			By("Apply rendered manifests to target")
			Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

			By("Verify app resources exist on target")
			_, err = kubectlTgt.Run("get", "deployment", appName+"-deployment", "-n", namespace)
			Expect(err).NotTo(HaveOccurred(), "Deployment should exist on target")
			_, err = kubectlTgt.Run("get", "service", "my-"+appName, "-n", namespace)
			Expect(err).NotTo(HaveOccurred(), "Service should exist on target")
			_, err = kubectlTgt.Run("get", "configmap", "app-settings", "-n", namespace)
			Expect(err).NotTo(HaveOccurred(), "ConfigMap should exist on target")

			By("Verify OLM resources do NOT exist on target")
			for _, res := range []struct {
				kind string
				name string
			}{
				{"subscription", "olm-whiteout-subscription"},
				{"catalogsource", "olm-whiteout-catalog"},
				{"operatorgroup", "olm-whiteout-og"},
			} {
				_, err := kubectlTgt.Run("get", res.kind, res.name, "-n", namespace)
				Expect(err).To(HaveOccurred(), "%s %s should not exist on target", res.kind, res.name)
				Expect(err.Error()).To(ContainSubstring("NotFound"))
			}
		})
	})
})
