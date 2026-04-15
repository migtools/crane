package e2e

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Empty PVC migration", func() {
	It("[MTC-153] Migrate an empty PVC associated with an application", Label("tier0"), func() {
		appName := "app-with-empty-pvc"
		namespace := appName
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		if scenario.KubectlSrcNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin test")
		}
		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin

		srcApp.ExtraVars = map[string]string{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = map[string]string{
			"non_admin_user": "true",
		}

		By("Grant ns admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanup)

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup temp directory")
			if paths.TempDir != "" {
				log.Printf("Removing temp dir: %s\n", paths.TempDir)
				if err := os.RemoveAll(paths.TempDir); err != nil {
					log.Printf("cleanup: failed to remove temp dir %q: %v", paths.TempDir, err)
				}
			}
		})
		DeferCleanup(func() {
			By("Delete test namespace on source and target (best effort)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=false"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		By("List pvcs in the source namespace")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).NotTo(BeEmpty(), "expected at least one pvc in source namespace %q", srcApp.Namespace)
		log.Printf("Found %d pvcs in namespace %q", len(pvcs), srcApp.Namespace)
		for _, pvc := range pvcs {
			log.Printf("Found pvc %s in namespace %q\n", pvc.Name, pvc.Namespace)
		}
		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		runner := scenario.Crane
		runner.WorkDir = paths.TempDir
		Expect(RunCranePipelineWithChecks(runner, srcApp.Namespace, paths)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for source namespace %s\n", srcApp.Namespace)

		By("Transfer PVCs")
		for _, pvc := range pvcs {
			pvcName := pvc.Name

			opts := TransferPVCOptions{
				SourceContext:   srcApp.Context,
				TargetContext:   tgtApp.Context,
				PVCName:         pvcName,
				PVCNamespaceMap: fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
				Endpoint:        "route",
				IngressClass:    "",
				Subdomain:       "",
			}
			log.Printf("Transferring PVC %s to namespace %s on target cluster", pvcName, tgtApp.Namespace)
			Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred())
			log.Printf("PVC transfer complete : %s -> namespace %s", pvcName, tgtApp.Namespace)
		}

		By("List pvcs on target cluster")
		tgtpvcs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtpvcs).NotTo(BeEmpty(), "expected at least one pvc in target namespace %q", tgtApp.Namespace)

		By("Remove OpenShift-injected resources and fix Pod security context")
		err = filepath.Walk(paths.OutputDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".yaml") {
				return nil
			}
			filename := filepath.Base(path)

			// Remove OpenShift auto-injected resources and RBAC bindings
			if strings.Contains(filename, "openshift-service-ca.crt") ||
				strings.Contains(filename, "crane-user-admin") ||
				strings.Contains(filename, "RoleBinding_system") ||
				strings.Contains(filename, "ServiceAccount_builder") ||
				strings.Contains(filename, "ServiceAccount_deployer") {
				log.Printf("Removing OpenShift-injected resource: %s\n", path)
				if err := os.Remove(path); err != nil {
					log.Printf("Warning: failed to remove %s: %v", path, err)
				}
				return nil
			}

			// Strip securityContext from Pod manifests (crane exports runtime UIDs that differ between clusters)
			if strings.Contains(filename, "Pod_") {
				log.Printf("Stripping securityContext from Pod manifest: %s\n", path)
				content, err := os.ReadFile(path)
				if err != nil {
					log.Printf("Warning: failed to read %s: %v", path, err)
					return nil
				}

				// Remove pod-level and container-level securityContext
				lines := strings.Split(string(content), "\n")
				var filtered []string
				skipUntilUnindent := false
				indentLevel := 0

				for _, line := range lines {
					trimmed := strings.TrimLeft(line, " ")
					currentIndent := len(line) - len(trimmed)

					if strings.HasPrefix(trimmed, "securityContext:") {
						skipUntilUnindent = true
						indentLevel = currentIndent
						continue
					}

					if skipUntilUnindent {
						if currentIndent <= indentLevel && trimmed != "" {
							skipUntilUnindent = false
						} else {
							continue
						}
					}

					filtered = append(filtered, line)
				}

				if err := os.WriteFile(path, []byte(strings.Join(filtered, "\n")), 0644); err != nil {
					log.Printf("Warning: failed to write %s: %v", path, err)
				}
			}

			return nil
		})
		Expect(err).NotTo(HaveOccurred())

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", tgtApp.Namespace, paths.OutputDir)
		Expect(kubectlTgtNonAdmin.ApplyDir(paths.OutputDir)).NotTo(HaveOccurred())

		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)

	})
})
