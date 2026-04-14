package e2e

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
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

// assertNoOLMWhiteoutKindsInOutput walks root (e.g. crane apply output) and fails if any
// manifest has a denied kind or if a file path suggests an OLM whiteout resource (same idea as MTC-127).
func assertNoOLMWhiteoutKindsInOutput(root string) error {
	denyKind := make(map[string]struct{}, len(olmWhiteoutKinds))
	for _, k := range olmWhiteoutKinds {
		denyKind[k] = struct{}{}
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		for _, kind := range olmWhiteoutKinds {
			if strings.Contains(rel, kind) {
				return fmt.Errorf("output path %q contains forbidden OLM kind substring %q", rel, kind)
			}
		}
		if !utils.LooksLikeYAMLFile(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(strings.TrimSpace(string(data))) == 0 {
			return nil
		}
		dec := yaml.NewDecoder(strings.NewReader(string(data)))
		for {
			var doc map[string]interface{}
			err := dec.Decode(&doc)
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("%s: parse yaml: %w", rel, err)
			}
			if doc == nil {
				continue
			}
			kindVal, _ := doc["kind"].(string)
			if kindVal == "" {
				continue
			}
			if _, bad := denyKind[kindVal]; bad {
				return fmt.Errorf("%s: document kind %q must not appear in crane output", rel, kindVal)
			}
		}
		return nil
	})
}

var _ = Describe("OLM whiteout", func() {
	Describe("Baseline full OLM graph", func() {
		It("should omit OLM kinds from crane apply output", Label("olm"), func() {
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

			By("Verify output directory does not contain OLM whiteout kinds")
			Expect(assertNoOLMWhiteoutKindsInOutput(paths.OutputDir)).NotTo(HaveOccurred())

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
})
