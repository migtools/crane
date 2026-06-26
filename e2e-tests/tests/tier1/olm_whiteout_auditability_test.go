package e2e

import (
	"log"
	"os"
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

	Describe("Transform-stage auditability", func() {
		It("should preserve whiteout resource files and comments in transform stage", Label("olm", "tier1"), func() {
			kubectlPreflight := KubectlRunner{Bin: "kubectl", Context: config.SourceContext}
			olmAvailable, err := kubectlPreflight.OLMAPIAvailable()
			Expect(err).NotTo(HaveOccurred())
			if !olmAvailable {
				Skip("OLM APIs not installed (subscriptions.operators.coreos.com CRD missing)")
			}

			namespace := "olm-audit"
			scenario := NewMigrationScenario(
				"olm-audit",
				namespace,
				config.K8sDeployBin,
				config.CraneBin,
				config.SourceContext,
				config.TargetContext,
			)
			kubectlSrc := scenario.KubectlSrc

			paths, err := NewScenarioPaths("crane-audit-*")
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
				By("Cleanup namespace and temp dir")
				if _, err := kubectlSrc.Run("delete", "namespace", namespace, "--ignore-not-found=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace: %v", err)
				}
				if paths.TempDir != "" {
					if err := os.RemoveAll(paths.TempDir); err != nil {
						log.Printf("cleanup: failed to remove temp dir %s: %v", paths.TempDir, err)
					}
				}
			})

			By("Create source namespace")
			Expect(kubectlSrc.CreateNamespace(namespace)).NotTo(HaveOccurred())

			By("Create OLM resources (OperatorGroup, CatalogSource, Subscription)")
			olmSpec, err := utils.ReadTestdataFile("olm-resources.yaml")
			Expect(err).NotTo(HaveOccurred())
			olmSpec = strings.ReplaceAll(olmSpec, "__NAMESPACE__", namespace)
			Expect(kubectlSrc.ApplyYAMLSpec(olmSpec, namespace)).NotTo(HaveOccurred())

			By("Create ConfigMap as non-OLM resource")
			configMapSpec, err := utils.ReadTestdataFile("app-configmap.yaml")
			Expect(err).NotTo(HaveOccurred())
			Expect(kubectlSrc.ApplyYAMLSpec(configMapSpec, namespace)).NotTo(HaveOccurred())

			runner := scenario.Crane
			runner.WorkDir = paths.TempDir

			By("Run crane export")
			Expect(runner.Export(ExportOptions{Namespace: namespace, ExportDir: paths.ExportDir})).NotTo(HaveOccurred())

			By("Run crane transform")
			Expect(runner.Transform(TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir})).NotTo(HaveOccurred())
			olmKindsDeployed := []string{"Subscription", "CatalogSource", "OperatorGroup"}

			By("Verify whiteout resource files exist in transform input/ directory")
			Expect(utils.AssertWhiteoutResourceFilesExist(paths.TransformDir, olmKindsDeployed)).NotTo(HaveOccurred())

			By("Verify kustomization.yaml has whiteout comments for OLM kinds")
			Expect(utils.AssertWhiteoutCommentsInKustomization(paths.TransformDir, olmKindsDeployed)).NotTo(HaveOccurred())

			By("Verify OLM kinds are NOT in active kustomization.yaml resources list")
			Expect(utils.AssertKindsNotInActiveKustomizeResources(paths.TransformDir, olmWhiteoutKinds)).NotTo(HaveOccurred())

			By("Run crane apply")
			Expect(runner.Apply(ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
				OutputDir: paths.OutputDir})).NotTo(HaveOccurred())

			By("Verify output does not contain OLM whiteout kinds")
			Expect(utils.AssertNoKindsInOutput(paths.OutputDir, olmWhiteoutKinds)).NotTo(HaveOccurred())

			By("Verify output contains ConfigMap (non-OLM resource)")
			Expect(utils.AssertKindsInOutput(paths.OutputDir, []string{"ConfigMap"})).NotTo(HaveOccurred())
		})
	})

	Describe("Multiple OLM operators", func() {
		It("should whiteout all OLM kinds from multiple operators in the same namespace", Label("olm", "tier1"), func() {
			kubectlPreflight := KubectlRunner{Bin: "kubectl", Context: config.SourceContext}
			olmAvailable, err := kubectlPreflight.OLMAPIAvailable()
			Expect(err).NotTo(HaveOccurred())
			if !olmAvailable {
				Skip("OLM APIs not installed (subscriptions.operators.coreos.com CRD missing)")
			}

			namespace := "olm-multi-op"
			scenario := NewMigrationScenario(
				"olm-multi-op",
				namespace,
				config.K8sDeployBin,
				config.CraneBin,
				config.SourceContext,
				config.TargetContext,
			)
			kubectlSrc := scenario.KubectlSrc

			paths, err := NewScenarioPaths("crane-multi-op-*")
			Expect(err).NotTo(HaveOccurred())
			applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
				OutputDir: paths.OutputDir}
			exportOpts := ExportOptions{Namespace: namespace, ExportDir: paths.ExportDir}
			transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
			DeferCleanup(func() {
				By("Cleanup OLM resources on source")
				for _, res := range []struct {
					kind string
					name string
				}{
					{"subscription", "olm-multi-sub-certmgr"},
					{"subscription", "olm-multi-sub-cockroachdb"},
					{"catalogsource", "olm-multi-catalog"},
					{"operatorgroup", "olm-multi-og"},
				} {
					if _, err := kubectlSrc.Run("delete", res.kind, res.name, "-n", namespace, "--ignore-not-found=true"); err != nil {
						log.Printf("cleanup: failed to delete %s/%s: %v", res.kind, res.name, err)
					}
				}
				By("Cleanup namespace and temp dir")
				if _, err := kubectlSrc.Run("delete", "namespace", namespace, "--ignore-not-found=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace: %v", err)
				}
				if paths.TempDir != "" {
					if err := os.RemoveAll(paths.TempDir); err != nil {
						log.Printf("cleanup: failed to remove temp dir %s: %v", paths.TempDir, err)
					}
				}
			})

			By("Create source namespace")
			Expect(kubectlSrc.CreateNamespace(namespace)).NotTo(HaveOccurred())

			By("Create OLM resources (OperatorGroup, CatalogSource, 2 Subscriptions)")
			olmSpec, err := utils.ReadTestdataFile("olm-multi-operator-resources.yaml")
			Expect(err).NotTo(HaveOccurred())
			olmSpec = strings.ReplaceAll(olmSpec, "__NAMESPACE__", namespace)
			Expect(kubectlSrc.ApplyYAMLSpec(olmSpec, namespace)).NotTo(HaveOccurred())

			By("Wait for both subscriptions to have currentCSV populated")
			for _, subName := range []string{"olm-multi-sub-certmgr", "olm-multi-sub-cockroachdb"} {
				Eventually(func(g Gomega) {
					out, err := kubectlSrc.Run("get", "subscription", subName, "-n", namespace, "-o", "jsonpath={.status.currentCSV}")
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(strings.TrimSpace(out)).NotTo(BeEmpty(), "subscription %s has no currentCSV yet", subName)
				}, "15m", "20s").Should(Succeed())
			}

			By("Wait for all InstallPlans to complete")
			Eventually(func(g Gomega) {
				out, err := kubectlSrc.Run("get", "installplan", "-n", namespace, "-o", "jsonpath={.items[*].status.phase}")
				g.Expect(err).NotTo(HaveOccurred())
				phases := strings.Fields(out)
				g.Expect(phases).NotTo(BeEmpty(), "no InstallPlans found")
				for _, p := range phases {
					g.Expect(p).To(Equal("Complete"), "InstallPlan in phase %q", p)
				}
			}, "15m", "20s").Should(Succeed())

			runner := scenario.Crane
			runner.WorkDir = paths.TempDir

			By("Run crane export/transform/apply pipeline")
			log.Printf("Running crane pipeline for namespace %s\n", namespace)
			Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

			By("Verify no OLM whiteout kinds in output")
			Expect(utils.AssertNoKindsInOutput(paths.OutputDir, olmWhiteoutKinds)).NotTo(HaveOccurred())

			By("Verify transform stage has whiteout resource files for deployed OLM kinds")
			Expect(utils.AssertWhiteoutResourceFilesExist(paths.TransformDir, []string{"Subscription", "CatalogSource", "OperatorGroup"})).NotTo(HaveOccurred())

			By("Verify both Subscription whiteout files exist (one per operator)")
			Expect(utils.AssertWhiteoutResourceFileCount(paths.TransformDir, "Subscription", 2)).NotTo(HaveOccurred())

			By("Verify no partial whiteout: all OLM kinds excluded from active kustomize resources")
			Expect(utils.AssertKindsNotInActiveKustomizeResources(paths.TransformDir, olmWhiteoutKinds)).NotTo(HaveOccurred())
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
			applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
				OutputDir: paths.OutputDir}
			exportOpts := ExportOptions{Namespace: namespace, ExportDir: paths.ExportDir}
			transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
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
			Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
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
