package e2e

import (
	"log"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Sets", func() {
	It("[MTC-196] Should migrate ReplicaSet, DaemonSet and StatefulSet as namespace-admin", Label("tier0"), func() {
		appName := "sets"
		namespace := "sets"
		daemonSetAppName := "hello-daemonset"
		replicaSetAppName := "frontend"
		statefulSetAppName := "hello"
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		if scenario.KubectlSrcNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin stateless migration test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin stateless migration test")
		}
		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin

		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
			"daemonset": map[string]any{
				"app_name": daemonSetAppName,
			},
			"replicaset": map[string]any{
				"app_name": replicaSetAppName,
				"replicas": 2,
			},
			"statefulset": map[string]any{
				"app_name": statefulSetAppName,
				"replicas": 1,
			},
		}

		tgtApp.ExtraVars = srcApp.ExtraVars

		By("Grant ns admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Delete test namespace on source and target (best effort)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=false"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(cleanup)
		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		runner.WorkDir = paths.TempDir

		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, namespace, paths)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Validate output directory contains DaemonSet , ReplicaSet and StatefulSet resource manifest")
		log.Println("Checking for DaemonSet resource manifest")
		daemonSetPattern := filepath.Join(paths.OutputDir, "resources", namespace, "DaemonSet_*.yaml")
		matches, err := filepath.Glob(daemonSetPattern)
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).NotTo(BeEmpty(), "Expected at least one DaemonSet manifest")
		log.Printf("DaemonSet resource present in output dir: %s\n", matches[0])

		log.Println("Checking for ReplicaSet resource manifest")
		replicaSetPattern := filepath.Join(paths.OutputDir, "resources", namespace, "ReplicaSet_*.yaml")
		matches, err = filepath.Glob(replicaSetPattern)
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).NotTo(BeEmpty(), "Expected at least one ReplicaSet manifest")
		log.Printf("ReplicaSet resource present in output dir: %s\n", matches[0])

		log.Println("Checking for StatefulSet resource manifest")
		statefulSetPattern := filepath.Join(paths.OutputDir, "resources", namespace, "StatefulSet_*.yaml")
		matches, err = filepath.Glob(statefulSetPattern)
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).NotTo(BeEmpty(), "Expected at least one StatefulSet manifest")
		log.Printf("StatefulSet resource present in output dir: %s\n", matches[0])

		By("Applying rendered manifests to target cluster")
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())
		log.Printf("Output applied to target successfully")

		By("Validate DaemonSet , ReplicaSet , StatefulSet running on target")
		log.Printf("Validating DaemonSet , ReplicaSet , StatefulSet running on target")
		Eventually(func() error {
			return tgtApp.Validate()
		}, "2m", "10s").Should(Succeed())
		log.Printf("DaemonSet , ReplicaSet , StatefulSet running on target validated successfully")

	})
})
