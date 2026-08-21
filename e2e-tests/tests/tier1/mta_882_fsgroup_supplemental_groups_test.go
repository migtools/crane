package e2e

import (
	"fmt"
	"log"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PVC transfer with fsGroup / supplementalGroups", func() {
	It("[MTA-882] should migrate group-restricted PVC data and preserve app SecurityContext", Label("tier1", "pvc-transfer"), func() {
		appName := "pvc-fsgroup"
		namespace := "mta-882-fsgroup"
		pvcName := appName + "-pvc"
		const (
			dataMountPath = "/data"
			secretFile    = "group-secret.txt"
			secretContent = "fsgroup-seed-data"
			modeFile      = "mode.txt"
			gidFile       = "gid.txt"
		)

		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = map[string]any{
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
		paths, err := NewScenarioPaths("crane-mta-882-*")
		Expect(err).NotTo(HaveOccurred())
		runner.WorkDir = paths.TempDir

		exportOpts := ExportOptions{Namespace: namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}

		DeferCleanup(func() {
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup apps/tempdir: %v", err)
			}
		})

		By("Deploy source app with fsGroup/supplementalGroups and seed group-restricted PVC data")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		By("Confirm the source workload is ready and the PVC exists")
		readyReplicas, err := kubectlSrcNonAdmin.Run(
			"get", "deploy", appName, "-n", namespace,
			"-o", "jsonpath={.status.readyReplicas}",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(StripKubectlWarnings(readyReplicas))).To(Equal("1"))

		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).NotTo(BeEmpty(), "expected at least one PVC in source namespace %q", srcApp.Namespace)
		_, err = kubectlSrcNonAdmin.Run("get", "pvc", pvcName, "-n", namespace)
		Expect(err).NotTo(HaveOccurred())

		srcPod, err := GetPodNameByLabel(kubectlSrcNonAdmin, namespace, "app="+appName)
		Expect(err).NotTo(HaveOccurred())
		Expect(srcPod).NotTo(BeEmpty())

		By("Assert source Deployment/Pod SecurityContext (fsGroup / supplementalGroups) before migration")
		srcDeploySC, err := getWorkloadSecurityContext(kubectlSrcNonAdmin, "deploy", appName, namespace)
		Expect(err).NotTo(HaveOccurred())
		srcPodSC, err := getWorkloadSecurityContext(kubectlSrcNonAdmin, "pod", srcPod, namespace)
		Expect(err).NotTo(HaveOccurred())
		assertFSGroupSecurityContext(srcDeploySC, srcPodSC, scenario.KubectlSrc.IsOpenShift())
		log.Printf("MTA-882 source securityContext: deploy=%+v pod=%+v\n", srcDeploySC, srcPodSC)

		By("Capture source group-restricted file content, mode, and gid")
		sourceSecret, err := readFileFromPod(kubectlSrcNonAdmin, namespace, srcPod, dataMountPath+"/"+secretFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceSecret).To(Equal(secretContent))

		sourceMode, err := readFileFromPod(kubectlSrcNonAdmin, namespace, srcPod, dataMountPath+"/"+modeFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceMode).To(Equal("640"))

		sourceGID, err := readFileFromPod(kubectlSrcNonAdmin, namespace, srcPod, dataMountPath+"/"+gidFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceGID).To(MatchRegexp(`^[0-9]+$`))
		log.Printf("MTA-882 source snapshot: secret=%q mode=%s gid=%s\n", sourceSecret, sourceMode, sourceGID)

		By("Quiesce the source app before export/transfer (scale Deployment to 0)")
		Expect(kubectlSrcNonAdmin.ScaleDeployment(namespace, appName, 0)).NotTo(HaveOccurred())
		Eventually(func() string {
			out, err := kubectlSrcNonAdmin.Run("get", "pods", "-n", namespace, "-l", "app="+appName, "-o", "name")
			Expect(err).NotTo(HaveOccurred())
			return strings.TrimSpace(StripKubectlWarnings(out))
		}, "2m", "5s").Should(BeEmpty())

		By("Run crane export/transform/apply pipeline so target gets the same SecurityContext via migrated manifests")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Transfer the PVC with crane transfer-pvc")
		tgtIP, err := GetClusterNodeIP(scenario.TgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		opts := TransferPVCOptions{
			SourceContext:   srcApp.Context,
			TargetContext:   tgtApp.Context,
			PVCName:         pvcName,
			PVCNamespaceMap: fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
			Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvcName, namespace, tgtIP),
		}
		Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred())

		By("Verify the destination PVC exists on target")
		tgtPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(VerifyPVCsExistByName(pvcs, tgtPVCs)).NotTo(HaveOccurred())

		By("Apply rendered manifests to the target cluster")
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale up the migrated app on target and validate group-restricted data access")
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
		Eventually(tgtApp.Validate, "5m", "10s").Should(Succeed())

		tgtPod, err := GetPodNameByLabel(kubectlTgtNonAdmin, namespace, "app="+appName)
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtPod).NotTo(BeEmpty())

		By("Assert target Deployment/Pod SecurityContext matches source (migrated manifests)")
		tgtDeploySC, err := getWorkloadSecurityContext(kubectlTgtNonAdmin, "deploy", appName, namespace)
		Expect(err).NotTo(HaveOccurred())
		tgtPodSC, err := getWorkloadSecurityContext(kubectlTgtNonAdmin, "pod", tgtPod, namespace)
		Expect(err).NotTo(HaveOccurred())
		assertFSGroupSecurityContext(tgtDeploySC, tgtPodSC, scenario.KubectlTgt.IsOpenShift())
		Expect(tgtDeploySC).To(Equal(srcDeploySC),
			"target Deployment securityContext must match source (crane export/transform/apply)")
		if !scenario.KubectlTgt.IsOpenShift() {
			Expect(tgtPodSC.FSGroup).To(Equal(srcPodSC.FSGroup))
			Expect(tgtPodSC.RunAsUser).To(Equal(srcPodSC.RunAsUser))
			Expect(tgtPodSC.RunAsGroup).To(Equal(srcPodSC.RunAsGroup))
			Expect(tgtPodSC.SupplementalGroups).To(Equal(srcPodSC.SupplementalGroups))
		}
		log.Printf("MTA-882 target securityContext: deploy=%+v pod=%+v\n", tgtDeploySC, tgtPodSC)

		By("Confirm target app can read migrated group-restricted data and write to the PVC")
		targetSecret, err := readFileFromPod(kubectlTgtNonAdmin, namespace, tgtPod, dataMountPath+"/"+secretFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(targetSecret).To(Equal(sourceSecret))

		targetMode, err := readFileFromPod(kubectlTgtNonAdmin, namespace, tgtPod, dataMountPath+"/"+modeFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(targetMode).To(Equal("640"))

		_, err = kubectlTgtNonAdmin.Run(
			"exec", "-n", namespace, tgtPod, "-c", "app", "--",
			"sh", "-c", "echo mta-882-write-ok > "+dataMountPath+"/mta-882-write.txt && test -s "+dataMountPath+"/mta-882-write.txt",
		)
		Expect(err).NotTo(HaveOccurred())
		writeProbe, err := readFileFromPod(kubectlTgtNonAdmin, namespace, tgtPod, dataMountPath+"/mta-882-write.txt")
		Expect(err).NotTo(HaveOccurred())
		Expect(writeProbe).To(Equal("mta-882-write-ok"))
	})
})

// workloadSecurityContext is a compact view of pod-level securityContext fields
// relevant to MTA-882 (fsGroup / supplementalGroups).
type workloadSecurityContext struct {
	FSGroup            string
	RunAsUser          string
	RunAsGroup         string
	SupplementalGroups string
}

func getWorkloadSecurityContext(k KubectlRunner, kind, name, namespace string) (workloadSecurityContext, error) {
	pathFmt := "jsonpath={.spec.securityContext.%s}"
	if kind == "deploy" || kind == "deployment" {
		kind = "deploy"
		pathFmt = "jsonpath={.spec.template.spec.securityContext.%s}"
	}

	readField := func(field string) (string, error) {
		out, err := k.Run("get", kind, name, "-n", namespace, "-o", fmt.Sprintf(pathFmt, field))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(StripKubectlWarnings(out)), nil
	}

	fsGroup, err := readField("fsGroup")
	if err != nil {
		return workloadSecurityContext{}, err
	}
	runAsUser, err := readField("runAsUser")
	if err != nil {
		return workloadSecurityContext{}, err
	}
	runAsGroup, err := readField("runAsGroup")
	if err != nil {
		return workloadSecurityContext{}, err
	}
	suppGroups, err := readField("supplementalGroups")
	if err != nil {
		return workloadSecurityContext{}, err
	}

	return workloadSecurityContext{
		FSGroup:            fsGroup,
		RunAsUser:          runAsUser,
		RunAsGroup:         runAsGroup,
		SupplementalGroups: suppGroups,
	}, nil
}

func assertFSGroupSecurityContext(deploySC, podSC workloadSecurityContext, isOpenShift bool) {
	if isOpenShift {
		// On OpenShift the Deployment may omit explicit IDs (SCC assigns them).
		// The running Pod must still end up with an fsGroup.
		Expect(podSC.FSGroup).To(MatchRegexp(`^[0-9]+$`),
			"OpenShift pod must have an SCC-assigned fsGroup")
		return
	}

	// Vanilla clusters: pvc-fsgroup sets explicit IDs (defaults to 1000).
	Expect(deploySC.FSGroup).To(Equal("1000"), "Deployment fsGroup")
	Expect(deploySC.RunAsUser).To(Equal("1000"), "Deployment runAsUser")
	Expect(deploySC.RunAsGroup).To(Equal("1000"), "Deployment runAsGroup")
	Expect(deploySC.SupplementalGroups).To(ContainSubstring("1000"), "Deployment supplementalGroups")

	Expect(podSC.FSGroup).To(Equal("1000"), "Pod fsGroup")
	Expect(podSC.RunAsUser).To(Equal("1000"), "Pod runAsUser")
	Expect(podSC.RunAsGroup).To(Equal("1000"), "Pod runAsGroup")
	Expect(podSC.SupplementalGroups).To(ContainSubstring("1000"), "Pod supplementalGroups")
}
