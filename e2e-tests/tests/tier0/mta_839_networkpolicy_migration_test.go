package e2e

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NetworkPolicy migration", func() {
	It("[MTA-839] NetworkPolicy is exported, transformed, and applied to target cluster", Label("tier0"), func() {
		appName := "simple-nginx-nopv"
		namespace := "test-netpol"
		deploymentName := appName + "-deployment"
		serviceName := "my-" + appName
		networkPolicyName := appName + "-policy"

		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		if scenario.KubectlSrcNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for namespace-admin NetworkPolicy migration test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for namespace-admin NetworkPolicy migration test")
		}
		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		By("Grant namespace-admin permissions to non-admin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanup)

		By("Prepare source app as namespace-admin")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		networkPolicyManifest := fmt.Sprintf(`apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: %s
  namespace: %s
spec:
  podSelector:
    matchLabels:
      app: %s
  policyTypes:
    - Ingress
    - Egress
  ingress:
    - from:
        - podSelector: {}
      ports:
        - protocol: TCP
          port: 8080
  egress:
    - to:
        - namespaceSelector: {}
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
`, networkPolicyName, namespace, appName)

		By("Create NetworkPolicy on source cluster")
		Expect(kubectlSrcNonAdmin.ApplyYAMLSpec(networkPolicyManifest, namespace)).NotTo(HaveOccurred())

		By("Verify NetworkPolicy exists on source before export")
		srcNetpolJSON, err := kubectlSrcNonAdmin.Run("get", "networkpolicy", networkPolicyName, "-n", namespace, "-o", "json")
		Expect(err).NotTo(HaveOccurred(), "NetworkPolicy should exist on source cluster")
		log.Printf("NetworkPolicy %s found on source cluster\n", networkPolicyName)

		var srcNetpol map[string]any
		Expect(json.Unmarshal([]byte(srcNetpolJSON), &srcNetpol)).NotTo(HaveOccurred())

		paths, err := NewScenarioPaths("crane-export-netpol-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		runner := scenario.CraneNonAdmin
		runner.WorkDir = paths.TempDir

		By("Wait for source quiesce to stabilize before export")
		WaitForSourceQuiesce(kubectlSrcNonAdmin, namespace, "app="+appName, serviceName)

		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Verify NetworkPolicy manifest is present in output directory")
		netpolGlob := filepath.Join(paths.OutputDir, "resources", namespace, "NetworkPolicy_*.yaml")
		netpolMatches, err := filepath.Glob(netpolGlob)
		Expect(err).NotTo(HaveOccurred())
		Expect(netpolMatches).NotTo(BeEmpty(), "expected NetworkPolicy manifest in output directory")
		log.Printf("NetworkPolicy manifest found in output: %v\n", netpolMatches)

		By("Apply rendered manifests to target as namespace-admin")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app")
		log.Printf("Scaling target deployment %s to 1\n", deploymentName)
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())

		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)

		By("Verify NetworkPolicy is present on target cluster")
		tgtNetpolJSON, err := kubectlTgtNonAdmin.Run("get", "networkpolicy", networkPolicyName, "-n", namespace, "-o", "json")
		Expect(err).NotTo(HaveOccurred(), "NetworkPolicy should be present on target cluster")
		log.Printf("NetworkPolicy %s found on target cluster\n", networkPolicyName)

		var tgtNetpol map[string]any
		Expect(json.Unmarshal([]byte(tgtNetpolJSON), &tgtNetpol)).NotTo(HaveOccurred())

		srcSpec, ok := srcNetpol["spec"].(map[string]any)
		Expect(ok).To(BeTrue(), "source NetworkPolicy spec should be a map")
		tgtSpec, ok := tgtNetpol["spec"].(map[string]any)
		Expect(ok).To(BeTrue(), "target NetworkPolicy spec should be a map")

		By("Verify NetworkPolicy spec matches source for key migration fields")
		Expect(tgtSpec["podSelector"]).To(Equal(srcSpec["podSelector"]))
		Expect(tgtSpec["policyTypes"]).To(Equal(srcSpec["policyTypes"]))
		Expect(tgtSpec["ingress"]).To(Equal(srcSpec["ingress"]))
		Expect(tgtSpec["egress"]).To(Equal(srcSpec["egress"]))
	})
})
