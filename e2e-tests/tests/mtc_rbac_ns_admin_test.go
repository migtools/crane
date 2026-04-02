package e2e

import (
	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RBAC Namespace Admin", func() {
	Context("When a user is granted namespace admin", func() {
		It("should be able to perform actions in the namespace", func() {
			namespace := "rbac-ns-admin"
			adminContext := config.SourceContext
			nonadminContext := config.SourceNonAdminContext

			Expect(nonadminContext).NotTo(BeEmpty(), "source-nonadmin-context is required")

			kubectlSrcAdmin := KubectlRunner{
				Bin:     "kubectl",
				Context: adminContext,
			}
			kubectlSrcNonAdmin := KubectlRunner{
				Bin:     "kubectl",
				Context: nonadminContext,
			}

			can, err := kubectlSrcAdmin.CanI("create", "deployments", namespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(can).To(BeTrue())

			can, err = kubectlSrcNonAdmin.CanI("create", "deployments", namespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(can).To(BeFalse(), "non-admin user should not be able to create deployments before rolebinding")

			By("Grant namespace admin to non-admin user")

			kubectlSrcNonAdmin, cleanup, err := SetupNamespaceAdminUser(kubectlSrcAdmin, nonadminContext, namespace)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(cleanup)

			can, err = kubectlSrcNonAdmin.CanI("create", "deployments", namespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(can).To(BeTrue(), "user with namespace admin role should be able to create deployments after rolebinding")

		})
	})
})
