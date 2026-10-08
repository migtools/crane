package e2e

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("transfer-pvc rsync failure reporting", func() {
	It("returns an error when rsync cannot transfer any source data", Label("tier1", "pvc-transfer", "direct"), func() {
		const (
			sourceNamespace = "crane-408-source"
			targetNamespace = "crane-408-target"
			pvcName         = "restricted-data"
			seedPodName     = "seed-restricted-data"
		)

		kubectl := KubectlRunner{Bin: "kubectl", Context: config.SourceContext}
		DeferCleanup(func() {
			for _, namespace := range []string{sourceNamespace, targetNamespace} {
				if _, err := kubectl.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=120s"); err != nil {
					log.Printf("cleanup namespace %q: %v", namespace, err)
				}
			}
		})

		paths, err := NewScenarioPaths("crane-issue-408-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			if err := os.RemoveAll(paths.TempDir); err != nil {
				log.Printf("cleanup temp directory %q: %v", paths.TempDir, err)
			}
		})

		By("Create isolated source and target namespaces")
		Expect(kubectl.CreateNamespace(sourceNamespace)).To(Succeed())
		Expect(kubectl.CreateNamespace(targetNamespace)).To(Succeed())

		By("Create a source PVC and a workload security context for UID 1000")
		manifest := fmt.Sprintf(`apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %[1]s
  namespace: %[2]s
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 16Mi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: restricted-data-owner
  namespace: %[2]s
spec:
  replicas: 0
  selector:
    matchLabels:
      app: restricted-data-owner
  template:
    metadata:
      labels:
        app: restricted-data-owner
    spec:
      securityContext:
        runAsUser: 1000
        runAsGroup: 1000
        fsGroup: 1000
      containers:
        - name: owner
          image: busybox:1.36
          command: ["sh", "-c", "sleep 3600"]
          volumeMounts:
            - name: data
              mountPath: /data
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: %[1]s
`, pvcName, sourceNamespace)
		Expect(kubectl.ApplyYAMLSpec(manifest, sourceNamespace)).To(Succeed())

		By("Seed only data that UID 1000 cannot read")
		seedManifest := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %[1]s
  namespace: %[2]s
spec:
  restartPolicy: Never
  containers:
    - name: seed
      image: busybox:1.36
      command:
        - sh
        - -c
        - |
          set -eux
          mkdir -p /data/restricted
          echo 'must-not-transfer' > /data/restricted/secret.txt
          chown -R 2000:2000 /data/restricted
          chmod 0700 /data/restricted
          chmod 0600 /data/restricted/secret.txt
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: %[3]s
`, seedPodName, sourceNamespace, pvcName)
		Expect(kubectl.ApplyYAMLSpec(seedManifest, sourceNamespace)).To(Succeed())
		_, err = kubectl.Run("wait", "--for=jsonpath={.status.phase}=Succeeded", "pod/"+seedPodName, "-n", sourceNamespace, "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectl.Run("delete", "pod", seedPodName, "-n", sourceNamespace, "--wait=true", "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())

		By("Run transfer-pvc and expect the rsync failure to reach the CLI")
		nodeIP, err := GetClusterNodeIP(config.SourceContext)
		Expect(err).NotTo(HaveOccurred())
		runner := CraneRunner{Bin: config.CraneBin, WorkDir: paths.TempDir}
		output, transferErr := runner.TransferPVCWithOutput(TransferPVCOptions{
			SourceContext:       config.SourceContext,
			TargetContext:       config.SourceContext,
			PVCName:             pvcName,
			PVCNamespaceMap:     fmt.Sprintf("%s:%s", sourceNamespace, targetNamespace),
			Subdomain:           fmt.Sprintf("%s.%s.%s.nip.io", pvcName, targetNamespace, nodeIP),
			DisableCloudStorage: true,
		})
		Expect(transferErr).To(HaveOccurred())
		Expect(output).To(MatchRegexp(`rsync client exited with code [1-9][0-9]*`))
		Expect(output).To(ContainSubstring("[6/7] Copying data (rsync) ... FAILED"))
		Expect(output).To(ContainSubstring("PVC data copy: failed"))
		Expect(output).NotTo(ContainSubstring("Done."))

		By("Verify the failed transfer still cleans up helper resources")
		AssertNoTransferPVCLeftovers(kubectl, []string{sourceNamespace, targetNamespace}, pvcName)
		names, err := kubectl.Run("get", "pvc", "-n", targetNamespace, "-o", "name")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(names)).To(Equal("persistentvolumeclaim/" + pvcName))
	})
})
