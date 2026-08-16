package e2e

import (
	"fmt"
	"log"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Multi-app namespace migration with pod quiesce", func() {
	It("[MTA-883] should migrate multiple apps in one namespace after quiesce", Label("tier1", "pvc-transfer"), func() {
		nginxAppName := "simple-nginx-nopv"
		redisAppName := "redis"
		namespace := "mta-883-multi-app"
		nginxServiceName := "my-" + nginxAppName

		scenario := NewMigrationScenario(
			nginxAppName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)

		srcNginx := scenario.SrcAppNonAdmin
		tgtNginx := scenario.TgtAppNonAdmin
		srcNginx.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtNginx.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		srcRedis := scenario.SrcAppNonAdmin
		srcRedis.Name = redisAppName
		srcRedis.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtRedis := scenario.TgtAppNonAdmin
		tgtRedis.Name = redisAppName
		tgtRedis.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		By("Grant namespace-admin permissions to non-admin users on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupActiveKubectlRunners(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(cleanup)

		runner := scenario.CraneNonAdmin
		paths, err := NewScenarioPaths("crane-mta-883-*")
		Expect(err).NotTo(HaveOccurred())
		runner.WorkDir = paths.TempDir

		exportOpts := ExportOptions{Namespace: namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}

		DeferCleanup(func() {
			if err := CleanupScenario(paths.TempDir, srcNginx, tgtNginx); err != nil {
				log.Printf("cleanup nginx apps/tempdir: %v", err)
			}
			if err := srcRedis.Cleanup(); err != nil {
				log.Printf("cleanup source redis app: %v", err)
			}
			if err := tgtRedis.Cleanup(); err != nil {
				log.Printf("cleanup target redis app: %v", err)
			}
		})

		By("Deploy both apps in the shared source namespace without quiescing")
		log.Printf("Preparing source app %s in namespace %s\n", srcNginx.Name, srcNginx.Namespace)
		Expect(PrepareSourceAppNoQuiesce(srcNginx)).NotTo(HaveOccurred())
		log.Printf("Preparing source app %s in namespace %s\n", srcRedis.Name, srcRedis.Namespace)
		Expect(PrepareSourceAppNoQuiesce(srcRedis)).NotTo(HaveOccurred())

		By("Quiesce all apps in the namespace (scale Deployments to 0)")
		Expect(kubectlSrcNonAdmin.ScaleDeployment(namespace, nginxAppName, 0)).NotTo(HaveOccurred())
		Expect(kubectlSrcNonAdmin.ScaleDeployment(namespace, redisAppName, 0)).NotTo(HaveOccurred())
		WaitForSourceQuiesce(kubectlSrcNonAdmin, namespace, "app="+nginxAppName, nginxServiceName)
		WaitForSourceQuiesce(kubectlSrcNonAdmin, namespace, "name="+redisAppName, redisAppName)

		By("Run crane export/transform/apply pipeline once for the shared namespace")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Transfer PVCs in the namespace to the target cluster")
		pvcs, err := ListPVCs(namespace, "", srcRedis.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).NotTo(BeEmpty(), "expected at least one PVC in source namespace %q", namespace)
		tgtIP, err := GetClusterNodeIP(scenario.TgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		for _, pvc := range pvcs {
			opts := TransferPVCOptions{
				SourceContext:   srcRedis.Context,
				TargetContext:   tgtRedis.Context,
				PVCName:         pvc.Name,
				PVCNamespaceMap: fmt.Sprintf("%s:%s", namespace, namespace),
				Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvc.Name, namespace, tgtIP),
			}
			log.Printf("Transferring PVC %s to namespace %s on target cluster", pvc.Name, namespace)
			Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred())
		}
		tgtPVCs, err := ListPVCs(namespace, "", tgtRedis.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(VerifyPVCsExistByName(pvcs, tgtPVCs)).NotTo(HaveOccurred())

		By("Apply rendered manifests to the target cluster")
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale up both apps on the target cluster and validate independently")
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, nginxAppName, 1)).NotTo(HaveOccurred())
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, redisAppName, 1)).NotTo(HaveOccurred())
		Eventually(tgtNginx.Validate, "5m", "10s").Should(Succeed())
		Eventually(tgtRedis.Validate, "5m", "10s").Should(Succeed())
	})
})
