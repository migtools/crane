package e2e

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Same-cluster transfer TLS", func() {
	It("[MTA-908] uses TLS/stunnel for direct same-cluster transfers", Label("tier1", "pvc-transfer"), func() {
		if config.CloudStorage != "" {
			Skip("MTA-908 validates direct rsync/stunnel transfers")
		}

		const (
			appName            = "mongodb"
			namespace          = "mta-908"
			sourcePVCName      = "mongodb-data"
			destinationPVCName = "mongodb-data-mta-908"
			fallbackDestSCName = "crane-dest-mta-908"
			roleLabel          = "app.konveyor.io/role"
			clientRole         = "client"
			serverRole         = "server"
		)

		// Both contexts intentionally use the source cluster. MTA-908 is not a
		// cross-cluster transfer test.
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.SourceContext,
		)
		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin
		isOCP := scenario.KubectlSrc.IsOpenShift()
		srcApp.ExtraVars = map[string]any{
			"non_admin_user": true,
			"has_scc":        isOCP,
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": true,
			"has_scc":        isOCP,
		}

		kubectl, cleanupNamespaceAdmin, err := SetupActiveNamespaceAdmin(
			scenario.KubectlSrc,
			scenario.KubectlSrcNonAdmin.Context,
			namespace,
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanupNamespaceAdmin)

		paths, err := NewScenarioPaths("crane-mta-908-*")
		Expect(err).NotTo(HaveOccurred())
		runner.WorkDir = paths.TempDir

		var cleanupDestSC func() error
		DeferCleanup(func() {
			By("Clean up MongoDB, temporary files, namespace, and destination StorageClass")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
			if _, err := scenario.KubectlSrc.Run(
				"delete", "namespace", namespace,
				"--ignore-not-found=true", "--wait=true", "--timeout=120s",
			); err != nil {
				log.Printf("cleanup namespace %q: %v", namespace, err)
			}
			if cleanupDestSC != nil {
				if err := cleanupDestSC(); err != nil {
					log.Printf("cleanup destination StorageClass: %v", err)
				}
			}
		})

		By("Deploy MongoDB and seed known data")
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())
		sourcePodName, err := GetPodNameByLabel(kubectl, namespace, "name="+appName)
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectl.Run(
			"exec", sourcePodName, "-n", namespace, "--",
			"mongosh", "sampledb",
			"--eval", `db.test_db.insertMany([{"a":1,"b":2},{"c":3,"d":4}])`,
			"--quiet",
		)
		Expect(err).NotTo(HaveOccurred())
		sourceDocumentCount, err := MongoDocumentCount(kubectl, namespace, sourcePodName)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceDocumentCount).To(BeNumerically(">", 0), "source MongoDB should contain seeded documents")

		By("Resolve the source StorageClass and prepare a distinct destination class")
		sourcePVC, err := GetPVC(srcApp.Context, namespace, sourcePVCName)
		Expect(err).NotTo(HaveOccurred())
		sourceSC, err := ResolvePVCStorageClass(scenario.SrcApp.Context, *sourcePVC)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceSC).NotTo(BeEmpty(), "source PVC must have a StorageClass")
		destinationSC, cleanupDestSC, err := PrepareDestinationStorageClass(
			scenario.SrcApp.Context,
			sourceSC,
			fallbackDestSCName,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(destinationSC).NotTo(Equal(sourceSC))

		By("Quiesce MongoDB before transferring its RWO PVC")
		Expect(kubectl.ScaleDeploymentIfPresent(namespace, appName, 0)).NotTo(HaveOccurred())
		Eventually(func() (string, error) {
			out, err := kubectl.Run("get", "pods", "-n", namespace, "-l", "name="+appName, "-o", "name")
			return StripKubectlWarnings(out), err
		}, "2m", "5s").Should(BeEmpty())

		By("Run direct same-cluster transfer and capture its live TLS resources")
		subdomain := ""
		if !kubectl.IsOpenShift() {
			nodeIP, err := GetClusterNodeIP(scenario.SrcApp.Context)
			Expect(err).NotTo(HaveOccurred())
			subdomain = fmt.Sprintf("%s.nip.io", nodeIP)
		}
		transferOpts := TransferPVCOptions{
			SourceContext:       srcApp.Context,
			TargetContext:       tgtApp.Context,
			PVCName:             fmt.Sprintf("%s:%s", sourcePVCName, destinationPVCName),
			PVCNamespaceMap:     fmt.Sprintf("%s:%s", namespace, namespace),
			DestStorageClass:    destinationSC,
			Subdomain:           subdomain,
			DisableCloudStorage: true,
		}
		transferDone := make(chan error, 1)
		go func() {
			transferDone <- runner.TransferPVC(transferOpts)
		}()

		resources := waitForSecureTransferResources(
			kubectl,
			namespace,
			sourcePVCName,
			destinationPVCName,
		)

		By("Verify client and server pods use stunnel and generated certificates")
		Expect(resources.clientPod.Labels[roleLabel]).To(Equal(clientRole))
		Expect(resources.serverPod.Labels[roleLabel]).To(Equal(serverRole))
		Expect(podHasContainer(resources.clientPod, "rsync")).To(BeTrue())
		Expect(podHasContainer(resources.clientPod, "stunnel")).To(BeTrue())
		Expect(podHasContainer(resources.serverPod, "rsync")).To(BeTrue())
		Expect(podHasContainer(resources.serverPod, "stunnel")).To(BeTrue())
		Expect(podHasSecretVolume(resources.clientPod, "stunnel-creds-certs-"+sourcePVCName)).To(BeTrue())
		Expect(podHasSecretVolume(resources.serverPod, "stunnel-creds-certs-"+destinationPVCName)).To(BeTrue())

		By("Verify stunnel configurations require TLS and certificate verification")
		clientConfig := resources.clientConfig.Data["stunnel.conf"]
		serverConfig := resources.serverConfig.Data["stunnel.conf"]
		Expect(clientConfig).To(ContainSubstring("client = yes"))
		Expect(clientConfig).To(ContainSubstring("sslVersion = TLSv1.3"))
		Expect(clientConfig).To(ContainSubstring("verify = 2"))
		Expect(clientConfig).To(ContainSubstring(":443"))
		Expect(serverConfig).To(ContainSubstring("sslVersion = TLSv1.3"))
		Expect(serverConfig).To(ContainSubstring("verify = 2"))
		Expect(serverConfig).To(ContainSubstring("accept = 6443"))
		Expect(serverConfig).To(ContainSubstring("connect = 8080"))

		By("Wait for the transfer to complete")
		Expect(<-transferDone).NotTo(HaveOccurred())

		By("Verify the destination PVC uses the requested StorageClass")
		destinationPVC, err := GetPVC(tgtApp.Context, namespace, destinationPVCName)
		Expect(err).NotTo(HaveOccurred())
		Expect(destinationPVC.Status.Phase).To(Equal(corev1.ClaimBound))
		Expect(PVCStorageClassName(*destinationPVC)).To(Equal(destinationSC))

		By("Verify the transferred MongoDB data on the destination PVC")
		verifyPod := "mta-908-mongo-verify"
		Expect(DeployVerifierPod(kubectl, VerifierPodOptions{
			Name:       verifyPod,
			Namespace:  namespace,
			Image:      "quay.io/migqe/mongo:7",
			Command:    []string{"mongod", "--dbpath", "/data/db", "--bind_ip_all"},
			Volumes:    []PodVolumeMount{{PVCName: destinationPVCName, MountPath: "/data/db"}},
			Restricted: true,
		})).NotTo(HaveOccurred())
		DeferCleanup(func() {
			if err := DeleteVerifierPod(kubectl, namespace, verifyPod); err != nil {
				log.Printf("cleanup verifier pod: %v", err)
			}
		})
		Eventually(func() (int, error) {
			return MongoDocumentCount(kubectl, namespace, verifyPod)
		}, "2m", "5s").Should(Equal(sourceDocumentCount))

		By("Confirm transfer helper resources are gone")
		AssertNoTransferPVCLeftovers(kubectl, []string{namespace}, sourcePVCName, destinationPVCName)
	})
})

type secureTransferResources struct {
	clientPod    corev1.Pod
	serverPod    corev1.Pod
	clientConfig corev1.ConfigMap
	serverConfig corev1.ConfigMap
}

func waitForSecureTransferResources(k KubectlRunner, namespace, sourcePVC, destinationPVC string) secureTransferResources {
	var resources secureTransferResources
	Eventually(func() error {
		if resources.clientPod.Name == "" {
			clientPods, err := listTransferPods(k, namespace, sourcePVC)
			if err != nil {
				return err
			}
			if len(clientPods) > 0 {
				resources.clientPod = clientPods[0]
			}
		}
		if resources.serverPod.Name == "" {
			serverPods, err := listTransferPods(k, namespace, destinationPVC)
			if err != nil {
				return err
			}
			if len(serverPods) > 0 {
				resources.serverPod = serverPods[0]
			}
		}
		if resources.clientConfig.Name == "" {
			clientConfigs, err := listStunnelConfigMaps(k, namespace, sourcePVC)
			if err != nil {
				return err
			}
			if len(clientConfigs) > 0 {
				resources.clientConfig = clientConfigs[0]
			}
		}
		if resources.serverConfig.Name == "" {
			serverConfigs, err := listStunnelConfigMaps(k, namespace, destinationPVC)
			if err != nil {
				return err
			}
			if len(serverConfigs) > 0 {
				resources.serverConfig = serverConfigs[0]
			}
		}
		if resources.clientPod.Name == "" || resources.serverPod.Name == "" || resources.clientConfig.Name == "" || resources.serverConfig.Name == "" {
			return fmt.Errorf("waiting for transfer resources: client pod=%t, server pod=%t, client config=%t, server config=%t",
				resources.clientPod.Name != "", resources.serverPod.Name != "", resources.clientConfig.Name != "", resources.serverConfig.Name != "")
		}
		return nil
	}, "3m", "500ms").Should(Succeed())
	return resources
}

func listTransferPods(k KubectlRunner, namespace, pvcName string) ([]corev1.Pod, error) {
	selector := "app.konveyor.io/created-for-pvc=" + pvcName
	out, err := k.Run("get", "pods", "-n", namespace, "-l", selector, "-o", "json")
	if err != nil {
		return nil, err
	}
	var list corev1.PodList
	if err := json.Unmarshal([]byte(StripKubectlWarnings(out)), &list); err != nil {
		return nil, fmt.Errorf("parse transfer pods for selector %q: %w", selector, err)
	}
	return list.Items, nil
}

func listStunnelConfigMaps(k KubectlRunner, namespace, pvcName string) ([]corev1.ConfigMap, error) {
	selector := "app.konveyor.io/created-for-pvc=" + pvcName
	out, err := k.Run("get", "configmaps", "-n", namespace, "-l", selector, "-o", "json")
	if err != nil {
		return nil, err
	}
	var list corev1.ConfigMapList
	if err := json.Unmarshal([]byte(StripKubectlWarnings(out)), &list); err != nil {
		return nil, fmt.Errorf("parse transfer ConfigMaps for selector %q: %w", selector, err)
	}
	var stunnelConfigs []corev1.ConfigMap
	for _, item := range list.Items {
		if strings.HasPrefix(item.Name, "stunnel-config-") {
			stunnelConfigs = append(stunnelConfigs, item)
		}
	}
	return stunnelConfigs, nil
}

func podHasContainer(pod corev1.Pod, name string) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == name {
			return true
		}
	}
	return false
}

func podHasSecretVolume(pod corev1.Pod, secretName string) bool {
	for _, volume := range pod.Spec.Volumes {
		if volume.Secret != nil && volume.Secret.SecretName == secretName {
			return true
		}
	}
	return false
}
