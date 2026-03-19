package e2e

import (
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("App deployment", func() {
	It("Deploys and validates app", func() {
		path, err := exec.LookPath(k8sdeployBin)
		Expect(err).NotTo(HaveOccurred())
		GinkgoWriter.Println("k8s deploy path:", path)
		app := K8sDeployApp{
			Name:      "simple-nginx-nopv",
			Namespace: "simple-nginx-nopv",
			Bin:       k8sdeployBin,
			Context:   sourceContext,
		}
		err = app.Deploy()
		Expect(err).NotTo(HaveOccurred())

		err = app.Validate()
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			err = app.Cleanup()
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
