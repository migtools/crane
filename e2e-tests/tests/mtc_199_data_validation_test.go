package e2e

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func buildPVCFixPodSpec(namespace, podName string, pvcNames []string) string {
	var b strings.Builder
	b.WriteString("apiVersion: v1\n")
	b.WriteString("kind: Pod\n")
	b.WriteString("metadata:\n")
	b.WriteString(fmt.Sprintf("  name: %s\n", podName))
	b.WriteString(fmt.Sprintf("  namespace: %s\n", namespace))
	b.WriteString("spec:\n")
	b.WriteString("  restartPolicy: Never\n")
	b.WriteString("  containers:\n")
	b.WriteString("  - name: fix\n")
	b.WriteString("    image: busybox\n")
	b.WriteString("    command: [\"sh\", \"-c\", \"sleep 300\"]\n")
	b.WriteString("    securityContext:\n")
	b.WriteString("      runAsUser: 0\n")
	b.WriteString("    volumeMounts:\n")
	for i := range pvcNames {
		b.WriteString(fmt.Sprintf("    - name: pvc-%d\n", i))
		b.WriteString(fmt.Sprintf("      mountPath: /mnt/pvc-%d\n", i))
	}
	b.WriteString("  volumes:\n")
	for i, pvcName := range pvcNames {
		b.WriteString(fmt.Sprintf("  - name: pvc-%d\n", i))
		b.WriteString("    persistentVolumeClaim:\n")
		b.WriteString(fmt.Sprintf("      claimName: %s\n", pvcName))
	}
	return b.String()
}

func runPVCFixCommands(k KubectlRunner, namespace, podName string, pvcNames []string, commands []string) error {
	if len(pvcNames) == 0 {
		return fmt.Errorf("no PVCs provided for fix pod")
	}
	spec := buildPVCFixPodSpec(namespace, podName, pvcNames)
	if err := k.ApplyYAMLSpec(spec, namespace); err != nil {
		return fmt.Errorf("create pvc fix pod %q: %w", podName, err)
	}
	defer func() {
		if _, err := k.Run("delete", "pod", podName, "-n", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
			log.Printf("cleanup: failed to delete pvc fix pod %q: %v", podName, err)
		}
	}()

	if _, err := k.Run(
		"wait", "pod", podName,
		"-n", namespace,
		"--for=condition=Ready",
		"--timeout=90s",
	); err != nil {
		return fmt.Errorf("wait for pvc fix pod %q ready: %w", podName, err)
	}

	cmd := strings.Join(commands, " && ")
	if _, err := k.Run("exec", podName, "-n", namespace, "--", "sh", "-c", cmd); err != nil {
		return fmt.Errorf("run pvc fix commands in %q: %w", podName, err)
	}
	return nil
}

func mysqlAuthorsCount(k KubectlRunner, namespace, podName string) (int, error) {
	out, err := k.Run(
		"exec", podName, "-n", namespace, "--",
		"sh", "-c",
		`MYSQL_PWD="$MYSQL_PASSWORD" mysql -N -B -u"$MYSQL_USER" "$MYSQL_DATABASE" -e "SELECT COUNT(*) FROM authors;"`,
	)
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parse authors count %q: %w", strings.TrimSpace(out), err)
	}
	return count, nil
}

func waitForMySQLSocket(k KubectlRunner, namespace, podName string) error {
	_, err := k.Run(
		"exec", podName, "-n", namespace, "--",
		"sh", "-c",
		`test -S /var/lib/mysql/mysql.sock`,
	)
	return err
}

func mysqlTestDataMD5(k KubectlRunner, namespace, podName string) (actual string, expected string, _ error) {
	actualOut, err := k.Run(
		"exec", podName, "-n", namespace, "--",
		"sh", "-c",
		`md5sum /test-data/test1 | awk '{print $1}'`,
	)
	if err != nil {
		return "", "", err
	}
	expectedOut, err := k.Run(
		"exec", podName, "-n", namespace, "--",
		"sh", "-c",
		`awk '{print $1}' /test-data/test1.md5`,
	)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(actualOut), strings.TrimSpace(expectedOut), nil
}

var _ = Describe("Data validation with indirect migration of MySQL DB", func() {

	It("[BUG #213][MTC-199] Should validate data", Label("BUG #213", "tier0"), func() {
		appName := "mysql"
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
			Skip("source-nonadmin-context is required for non-admin stateful migration test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin stateful migration test")
		}
		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin
		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		By("Grant namespace admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			By("Delete test namespace on source and target (wait for completion)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(cleanup)

		By("Deploy and validate source MySQL app")
		log.Printf("Deploying %s in namespace %s on source cluster", appName, namespace)
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())
		log.Printf("Source app deployed successfully")
		By("Capture source data fingerprints for comparison")
		srcPodName, err := GetPodNameByLabel(kubectlSrcNonAdmin, srcApp.Namespace, "app="+appName)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() error {
			return waitForMySQLSocket(kubectlSrcNonAdmin, srcApp.Namespace, srcPodName)
		}, "2m", "5s").Should(Succeed())
		sourceAuthorsCount, err := mysqlAuthorsCount(kubectlSrcNonAdmin, srcApp.Namespace, srcPodName)
		Expect(err).NotTo(HaveOccurred())
		sourceMD5Actual, sourceMD5Expected, err := mysqlTestDataMD5(kubectlSrcNonAdmin, srcApp.Namespace, srcPodName)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Source validation output: pod=%s authors_count=%d md5_actual=%s md5_expected=%s", srcPodName, sourceAuthorsCount, sourceMD5Actual, sourceMD5Expected)
		Expect(sourceMD5Actual).To(Equal(sourceMD5Expected), "source test-data checksum should match its md5 file")
		log.Printf("Source fingerprints: authors=%d md5=%s", sourceAuthorsCount, sourceMD5Actual)

		By("Quiesce source app before export")
		Expect(kubectlSrcNonAdmin.ScaleDeploymentIfPresent(srcApp.Namespace, srcApp.Name, 0)).NotTo(HaveOccurred())

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})
		By("List pvcs in the namespace")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).NotTo(BeEmpty(), "expected at least one pvc in namespace %q", srcApp.Namespace)
		log.Printf("Found %d pvcs in namespace %q", len(pvcs), srcApp.Namespace)
		pvcNames := make([]string, 0, len(pvcs))
		for _, pvc := range pvcs {
			log.Printf("Found pvc %s in namespace %q\n", pvc.Name, pvc.Namespace)
			pvcNames = append(pvcNames, pvc.Name)
		}

		By("Wait for source quiesce to stabilize before export")
		Eventually(func() (string, error) {
			out, err := kubectlSrcNonAdmin.Run(
				"get", "pods",
				"--namespace", namespace,
				"-l", "app="+appName,
				"-o", "name",
			)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(out), nil
		}, "90s", "3s").Should(BeEmpty())

		// Temporary workaround for BUG #213: remove once issue is fixed.
		// See: https://github.com/migtools/crane/issues/213
		By("[BUG #213] Relax source PVC permissions before transfer")
		sourceFixCommands := make([]string, 0, len(pvcNames)*2)
		for i := range pvcNames {
			sourceFixCommands = append(sourceFixCommands,
				fmt.Sprintf("if [ -d /mnt/pvc-%d/data ]; then chmod -R a+rX /mnt/pvc-%d/data; fi", i, i),
				fmt.Sprintf("chmod -R a+rX /mnt/pvc-%d", i),
			)
		}
		Expect(runPVCFixCommands(
			kubectlSrcNonAdmin,
			srcApp.Namespace,
			"mysql-source-pvc-perm-fix",
			pvcNames,
			sourceFixCommands,
		)).NotTo(HaveOccurred())

		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		runner.WorkDir = paths.TempDir
		Expect(RunCranePipelineWithChecks(runner, srcApp.Namespace, paths)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Transfer PVCs")
		tgtIP, err := GetClusterNodeIP(scenario.TgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		for _, pvc := range pvcs {
			pvcName := pvc.Name
			opts := TransferPVCOptions{
				SourceContext:   srcApp.Context,
				TargetContext:   tgtApp.Context,
				PVCName:         pvcName,
				PVCNamespaceMap: fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
				Endpoint:        "nginx-ingress",
				IngressClass:    "nginx",
				Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvcName, srcApp.Namespace, tgtIP),
			}
			log.Printf("Transferring PVC %s to namespace %s on target cluster", pvcName, tgtApp.Namespace)
			Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred())
			log.Printf("PVC transfer complete : %s -> namespace %s", pvcName, tgtApp.Namespace)
		}

		// Temporary workaround for BUG #213: remove once issue is fixed.
		// See: https://github.com/migtools/crane/issues/213
		By("[BUG #213] Restore destination PVC ownership for mysql runtime user")
		targetFixCommands := make([]string, 0, len(pvcNames)*2)
		for i := range pvcNames {
			targetFixCommands = append(targetFixCommands,
				fmt.Sprintf("if [ -d /mnt/pvc-%d/data ]; then chown -R 27:27 /mnt/pvc-%d/data && chmod -R u+rwX /mnt/pvc-%d/data; fi", i, i, i),
				fmt.Sprintf("chown -R 27:27 /mnt/pvc-%d && chmod -R u+rwX /mnt/pvc-%d", i, i),
			)
		}
		Expect(runPVCFixCommands(
			kubectlTgtNonAdmin,
			tgtApp.Namespace,
			"mysql-target-pvc-owner-fix",
			pvcNames,
			targetFixCommands,
		)).NotTo(HaveOccurred())

		By("List PVCs on target cluster")
		tgtpvcs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtpvcs).NotTo(BeEmpty(), "expected at least one PVC in target namespace %q", tgtApp.Namespace)
		Expect(VerifyPVCsExistByName(pvcs, tgtpvcs)).NotTo(HaveOccurred())
		log.Printf("Found %d PVCs in target namespace %q", len(tgtpvcs), tgtApp.Namespace)

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", tgtApp.Namespace, paths.OutputDir)
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app")
		log.Printf("Scaling target deployment(s) with label app=%s to 1\n", appName)
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())

		By("Validate target application")
		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		var tgtPodName string
		Eventually(func() error {
			podName, err := GetPodNameByLabel(kubectlTgtNonAdmin, tgtApp.Namespace, "app="+appName)
			if err != nil {
				return err
			}
			tgtPodName = podName
			out, err := kubectlTgtNonAdmin.Run(
				"get", "pod", tgtPodName,
				"-n", tgtApp.Namespace,
				"-o", "jsonpath={.status.containerStatuses[0].ready}",
			)
			if err != nil {
				return err
			}
			if strings.TrimSpace(out) != "true" {
				return fmt.Errorf("pod %s is not ready yet", tgtPodName)
			}
			return nil
		}, "2m", "10s").Should(Succeed())
		Eventually(func() error {
			return waitForMySQLSocket(kubectlTgtNonAdmin, tgtApp.Namespace, tgtPodName)
		}, "2m", "5s").Should(Succeed())

		targetAuthorsCount, err := mysqlAuthorsCount(kubectlTgtNonAdmin, tgtApp.Namespace, tgtPodName)
		Expect(err).NotTo(HaveOccurred())
		targetMD5Actual, targetMD5Expected, err := mysqlTestDataMD5(kubectlTgtNonAdmin, tgtApp.Namespace, tgtPodName)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Target validation output: pod=%s authors_count=%d md5_actual=%s md5_expected=%s", tgtPodName, targetAuthorsCount, targetMD5Actual, targetMD5Expected)
		Expect(targetMD5Actual).To(Equal(targetMD5Expected), "target test-data checksum should match its md5 file")

		Expect(targetAuthorsCount).To(Equal(sourceAuthorsCount), "authors count should match between source and target")
		Expect(targetMD5Actual).To(Equal(sourceMD5Actual), "test-data md5 should match between source and target")
		log.Printf("Target fingerprints: authors=%d md5=%s", targetAuthorsCount, targetMD5Actual)
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)
	})
})
