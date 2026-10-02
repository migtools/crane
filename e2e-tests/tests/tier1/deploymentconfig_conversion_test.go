package e2e

import (
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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var _ = Describe("OpenShift DeploymentConfig conversion", func() {
	It("converts a DeploymentConfig into a target-compatible Deployment", Label("tier1", "plugin"), func() {
		if config.PluginDir == "" {
			Skip("requires --plugin-dir containing the custom OpenShiftPlugin build")
		}

		const namespace = "deploymentconfig-conversion"
		paths, err := NewScenarioPaths("crane-deploymentconfig-conversion-*")
		Expect(err).NotTo(HaveOccurred())
		kubectlTgt := KubectlRunner{Bin: "kubectl", Context: config.TargetContext}
		DeferCleanup(func() {
			if _, err := kubectlTgt.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=60s"); err != nil {
				log.Printf("cleanup target namespace: %v", err)
			}
			if err := os.RemoveAll(paths.TempDir); err != nil {
				log.Printf("cleanup temp directory %q: %v", paths.TempDir, err)
			}
		})

		By("Create a synthetic export for the OpenShift-only source resource")
		exportDir, err := utils.TestdataFilePath("deploymentconfig-conversion/export")
		Expect(err).NotTo(HaveOccurred())

		By("Transform the DeploymentConfig with the custom OpenShiftPlugin")
		runner := CraneRunner{Bin: config.CraneBin, WorkDir: paths.TempDir}
		Expect(runner.Transform(TransformOptions{
			ExportDir:     exportDir,
			TransformDir:  paths.TransformDir,
			PluginDir:     config.PluginDir,
			OptionalFlags: `{"pvc-rename-map":"legacy-data:migrated-data"}`,
			Stages:        []string{"OpenShiftPlugin"},
		})).To(Succeed())
		Expect(runner.Apply(ApplyOptions{
			TransformDir: paths.TransformDir,
			OutputDir:    paths.OutputDir,
		})).To(Succeed())

		By("Verify the rendered output contains only the converted Deployment")
		output, err := os.ReadFile(filepath.Join(paths.OutputDir, "output.yaml"))
		Expect(err).NotTo(HaveOccurred())
		resources := decodeOutputResources(output)
		expectedOutput, err := utils.ReadTestdataFile("deploymentconfig-conversion/expected/output.yaml")
		Expect(err).NotTo(HaveOccurred())
		Expect(resources).To(Equal(decodeOutputResources([]byte(expectedOutput))))
		Expect(resources).To(HaveLen(1))
		deployment := resources[0]
		Expect(deployment.GetAPIVersion()).To(Equal("apps/v1"))
		Expect(deployment.GetKind()).To(Equal("Deployment"))
		Expect(deployment.GetName()).To(Equal("legacy-web"))
		Expect(deployment.GetNamespace()).To(Equal(namespace))
		Expect(deployment.GetAnnotations()).To(HaveKeyWithValue("example.com/preserved", "true"))
		Expect(deployment.GetAnnotations()["crane.konveyor.io/deploymentconfig-conversion-warnings"]).To(ContainSubstring("ImageChange trigger was removed"))

		replicas, found, err := unstructured.NestedInt64(deployment.Object, "spec", "replicas")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(replicas).To(BeZero())
		volumes, found, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "volumes")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(volumes).To(HaveLen(1))
		volume, ok := volumes[0].(map[string]interface{})
		Expect(ok).To(BeTrue())
		claim, ok := volume["persistentVolumeClaim"].(map[string]interface{})
		Expect(ok).To(BeTrue())
		claimName, ok := claim["claimName"].(string)
		Expect(ok).To(BeTrue())
		Expect(claimName).To(Equal("migrated-data"))
		_, found, err = unstructured.NestedFieldNoCopy(deployment.Object, "spec", "template", "spec", "securityContext", "runAsUser")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeFalse())

		By("Apply the converted resource and verify it on the target cluster")
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).To(Succeed())
		actual, err := kubectlTgt.Run(
			"get", "deployment", "legacy-web", "-n", namespace,
			"-o", "jsonpath={.apiVersion}|{.kind}|{.spec.replicas}|{.spec.template.spec.volumes[0].persistentVolumeClaim.claimName}",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal("apps/v1|Deployment|0|migrated-data"))
	})
})

func decodeOutputResources(content []byte) []unstructured.Unstructured {
	decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(content)), 4096)
	var resources []unstructured.Unstructured
	for {
		var resource unstructured.Unstructured
		err := decoder.Decode(&resource)
		if err == io.EOF {
			break
		}
		Expect(err).NotTo(HaveOccurred())
		if len(resource.Object) > 0 {
			resources = append(resources, resource)
		}
	}
	return resources
}
