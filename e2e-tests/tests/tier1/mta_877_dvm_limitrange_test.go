package e2e

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// MTA-877 verifies that DVM rsync pods respect LimitRange constraints.
const (
	limitRangeName = "dvm-container-limits"
	maxCPULimit    = "500m"
	maxMemoryLimit = "512Mi"
)

var _ = Describe("DVM pod LimitRange compliance", func() {
	It("[MTA-877] DVM pod limits should never breach the CPU and memory LimitRange values", Label("tier1", "pvc-transfer"), func() {
		if config.CloudStorage != "" {
			Skip("MTA-877 asserts DVM rsync pod LimitRange compliance; skip in indirect (rclone) transfer mode")
		}

		// pvc app already passes kube context in ansible; app-with-empty-pvc does not.
		appName := "pvc"
		namespace := "mta-877-limitrange"
		pvcName := "data-pvc"
		seedPodName := "seed-dvm-limitrange"
		verifyPodName := "verify-dvm-limitrange"
		maxCPU := resource.MustParse(maxCPULimit)
		maxMemory := resource.MustParse(maxMemoryLimit)

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
			"pvc_name":       pvcName,
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
			"pvc_name":       pvcName,
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
		paths, err := NewScenarioPaths("crane-mta-877-*")
		Expect(err).NotTo(HaveOccurred())
		runner.WorkDir = paths.TempDir
		DeferCleanup(func() {
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup apps/tempdir: %v", err)
			}
		})

		By("Create a LimitRange defining max CPU/memory per container on source and target namespaces")
		limitRangeYAML := containerLimitRangeManifest(limitRangeName, maxCPULimit, maxMemoryLimit)
		// Namespace-admin cannot create LimitRange on OpenShift; use cluster-admin.
		Expect(scenario.KubectlSrc.ApplyYAMLSpec(limitRangeYAML, namespace)).NotTo(HaveOccurred())
		Expect(scenario.KubectlTgt.ApplyYAMLSpec(limitRangeYAML, namespace)).NotTo(HaveOccurred())
		_, err = scenario.KubectlSrc.Run("get", "limitrange", limitRangeName, "-n", namespace)
		Expect(err).NotTo(HaveOccurred())
		_, err = scenario.KubectlTgt.Run("get", "limitrange", limitRangeName, "-n", namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Deploy a source app with a PVC containing data")
		Expect(srcApp.Deploy()).NotTo(HaveOccurred())
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).NotTo(BeEmpty(), "expected at least one PVC in source namespace %q", srcApp.Namespace)
		_, err = kubectlSrcNonAdmin.Run("get", "pvc", pvcName, "-n", namespace)
		Expect(err).NotTo(HaveOccurred())

		Expect(kubectlSrcNonAdmin.ApplyYAMLSpec(SeedPodManifest(namespace, seedPodName, pvcName), namespace)).NotTo(HaveOccurred())
		_, err = kubectlSrcNonAdmin.Run("wait", "--for=condition=Ready", "pod/"+seedPodName, "-n", namespace, "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		sourceHello, err := ReadFileFromPod(kubectlSrcNonAdmin, namespace, seedPodName, "/data/hello.txt")
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceHello).To(Equal("hello-from-source"))

		By("Delete the seed pod so the DVM rsync client can attach the PVC")
		_, err = kubectlSrcNonAdmin.Run("delete", "pod", seedPodName, "-n", namespace, "--wait=true")
		Expect(err).NotTo(HaveOccurred())

		By("Run crane transfer-pvc in the background so we can inspect rsync pods before they are deleted")
		tgtIP, err := GetClusterNodeIP(scenario.TgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		opts := TransferPVCOptions{
			SourceContext:   srcApp.Context,
			TargetContext:   tgtApp.Context,
			PVCName:         pvcName,
			PVCNamespaceMap: fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
			Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvcName, namespace, tgtIP),
		}

		// TransferPVC blocks until the copy is done, then deletes the rsync pods.
		// Run it in the background and inspect those pods from this test while they exist.
		transferDone := make(chan error, 1)
		go func() {
			transferDone <- runner.TransferPVC(opts)
		}()

		By("Wait until the rsync client (source) and rsync server (target) pods exist")
		var clientPod, serverPod corev1.Pod
		Eventually(func() error {
			var getErr error
			clientPod, getErr = getPodByNamePrefix(kubectlSrcNonAdmin, namespace, "rsync-client-")
			if getErr != nil {
				return getErr
			}
			serverPod, getErr = getPodByNamePrefix(kubectlTgtNonAdmin, namespace, "rsync-server-")
			return getErr
		}, "3m", "2s").Should(Succeed())

		By("Inspect rsync client and server pod CPU/memory against the LimitRange max")
		assertPodWithinLimitRange("source", clientPod, maxCPU, maxMemory)
		assertPodWithinLimitRange("target", serverPod, maxCPU, maxMemory)

		By("Wait for transfer-pvc to finish")
		Expect(<-transferDone).NotTo(HaveOccurred(),
			"transfer-pvc must complete without LimitRange admission errors")

		By("Confirm the destination PVC exists and the seeded file is intact")
		tgtPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(VerifyPVCsExistByName(pvcs, tgtPVCs)).NotTo(HaveOccurred())

		Expect(kubectlTgtNonAdmin.ApplyYAMLSpec(VerifyPodManifest(namespace, verifyPodName, pvcName), namespace)).NotTo(HaveOccurred())
		_, err = kubectlTgtNonAdmin.Run("wait", "--for=condition=Ready", "pod/"+verifyPodName, "-n", namespace, "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		destHello, err := ReadFileFromPod(kubectlTgtNonAdmin, namespace, verifyPodName, "/data/hello.txt")
		Expect(err).NotTo(HaveOccurred())
		Expect(destHello).To(Equal(sourceHello), "destination file must match source after DVM under LimitRange")
	})
})

func containerLimitRangeManifest(name, maxCPU, maxMemory string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: LimitRange
metadata:
  name: %s
spec:
  limits:
    - type: Container
      max:
        cpu: %s
        memory: %s
      default:
        cpu: %s
        memory: %s
      defaultRequest:
        cpu: 100m
        memory: 128Mi
`, name, maxCPU, maxMemory, maxCPU, maxMemory)
}

// getPodByNamePrefix returns the first pod in the namespace whose name starts with prefix.
func getPodByNamePrefix(k KubectlRunner, namespace, prefix string) (corev1.Pod, error) {
	out, err := k.Run("get", "pods", "-n", namespace, "-o", "json")
	if err != nil {
		return corev1.Pod{}, err
	}
	var list corev1.PodList
	if err := json.Unmarshal([]byte(StripKubectlWarnings(out)), &list); err != nil {
		return corev1.Pod{}, fmt.Errorf("parse pods in namespace %q: %w", namespace, err)
	}
	for _, pod := range list.Items {
		if strings.HasPrefix(pod.Name, prefix) {
			return pod, nil
		}
	}
	return corev1.Pod{}, fmt.Errorf("no pod starting with %q in namespace %q", prefix, namespace)
}

// assertPodWithinLimitRange checks every container in the pod has CPU and memory
// request/limit set (LimitRange injects defaults) and that none exceed maxCPU/maxMemory.
func assertPodWithinLimitRange(cluster string, pod corev1.Pod, maxCPU, maxMemory resource.Quantity) {
	GinkgoHelper()
	Expect(pod.Spec.Containers).NotTo(BeEmpty(), "DVM pod %s on %s should have containers", pod.Name, cluster)
	for _, c := range pod.Spec.Containers {
		log.Printf("MTA-877 %s DVM pod %s container %s requests=%v limits=%v (max cpu=%s memory=%s)\n",
			cluster, pod.Name, c.Name, c.Resources.Requests, c.Resources.Limits, maxCPU.String(), maxMemory.String())

		reqCPU, hasReqCPU := c.Resources.Requests[corev1.ResourceCPU]
		reqMem, hasReqMem := c.Resources.Requests[corev1.ResourceMemory]
		limCPU, hasLimCPU := c.Resources.Limits[corev1.ResourceCPU]
		limMem, hasLimMem := c.Resources.Limits[corev1.ResourceMemory]

		Expect(hasReqCPU && !reqCPU.IsZero()).To(BeTrue(), "%s pod %s container %s missing cpu request", cluster, pod.Name, c.Name)
		Expect(hasReqMem && !reqMem.IsZero()).To(BeTrue(), "%s pod %s container %s missing memory request", cluster, pod.Name, c.Name)
		Expect(hasLimCPU && !limCPU.IsZero()).To(BeTrue(), "%s pod %s container %s missing cpu limit", cluster, pod.Name, c.Name)
		Expect(hasLimMem && !limMem.IsZero()).To(BeTrue(), "%s pod %s container %s missing memory limit", cluster, pod.Name, c.Name)

		Expect(reqCPU.Cmp(maxCPU)).To(BeNumerically("<=", 0), "%s pod %s container %s cpu request %s exceeds max %s", cluster, pod.Name, c.Name, reqCPU.String(), maxCPU.String())
		Expect(reqMem.Cmp(maxMemory)).To(BeNumerically("<=", 0), "%s pod %s container %s memory request %s exceeds max %s", cluster, pod.Name, c.Name, reqMem.String(), maxMemory.String())
		Expect(limCPU.Cmp(maxCPU)).To(BeNumerically("<=", 0), "%s pod %s container %s cpu limit %s exceeds max %s", cluster, pod.Name, c.Name, limCPU.String(), maxCPU.String())
		Expect(limMem.Cmp(maxMemory)).To(BeNumerically("<=", 0), "%s pod %s container %s memory limit %s exceeds max %s", cluster, pod.Name, c.Name, limMem.String(), maxMemory.String())
	}
}
