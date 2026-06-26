package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	cranevalidate "github.com/konveyor/crane/internal/validate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Validate single incompatible Route [Live Mode]", func() {
	It("[MTA-846] should fail validate for route.openshift.io/v1 Route on minikube", Label("tier0", "validate"), func() {
		namespace := "validate-single-incompatible-route-live"
		scenario := NewMigrationScenario(
			"simple-nginx-nopv",
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		if scenario.KubectlTgt.IsOpenShift() {
			Skip("Route is supported on OpenShift targets; this minikube-only incompatibility check is not applicable")
		}

		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required")
		}

		runner := scenario.CraneNonAdmin
		paths, err := NewScenarioPaths("crane-validate-single-route-live-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(os.RemoveAll(paths.TempDir)).To(Succeed())
		})
		runner.WorkDir = paths.TempDir
		inputDir := filepath.Join(paths.TempDir, "input")
		Expect(os.MkdirAll(inputDir, 0o755)).NotTo(HaveOccurred())

		routeManifest := fmt.Sprintf(`apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: sample-route
  namespace: %s
spec:
  to:
    kind: Service
    name: sample-service
  port:
    targetPort: 8080
`, namespace)

		manifestPath := filepath.Join(inputDir, "route.yaml")
		Expect(os.WriteFile(manifestPath, []byte(routeManifest), 0o644)).NotTo(HaveOccurred())

		stdout, err := runner.Validate(
			ValidateOptions{
				Context:     scenario.KubectlTgtNonAdmin.Context,
				InputDir:    inputDir,
				ValidateDir: paths.ValidateDir,
			},
		)
		Expect(err).To(HaveOccurred(), "validate should fail for incompatible Route")
		Expect(stdout).To(ContainSubstring("Mode: live"))
		Expect(stdout).To(ContainSubstring("route.openshift.io/v1"))
		Expect(stdout).To(ContainSubstring("Incompatible"))
		Expect(stdout).To(ContainSubstring("Result: FAILED"))

		reportPath := filepath.Join(paths.ValidateDir, "report.json")
		Expect(reportPath).To(BeAnExistingFile())

		reportBytes, err := os.ReadFile(reportPath)
		Expect(err).NotTo(HaveOccurred())

		var report cranevalidate.ValidationReport
		Expect(json.Unmarshal(reportBytes, &report)).To(Succeed())

		Expect(report.Mode).To(Equal("live"))
		Expect(report.TotalScanned).To(Equal(1))
		Expect(report.Compatible).To(Equal(0))
		Expect(report.Incompatible).To(Equal(1))
		Expect(report.Results).To(HaveLen(1))

		result := report.Results[0]
		Expect(result.APIVersion).To(Equal("route.openshift.io/v1"))
		Expect(result.Kind).To(Equal("Route"))
		Expect(result.Namespace).To(Equal(namespace))
		Expect(result.Status).To(Equal(cranevalidate.StatusIncompatible))
		Expect(result.Reason).To(ContainSubstring("not available on target cluster"))
		Expect(result.Suggestion).To(BeEmpty(), "Route should not get alternative GV suggestion on minikube")

		failuresDir := filepath.Join(paths.ValidateDir, "failures")
		Expect(failuresDir).To(BeADirectory())

		failureFiles, err := filepath.Glob(filepath.Join(failuresDir, "*.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(failureFiles).To(HaveLen(1), "expected exactly one failure file for single incompatible Route")

		failureBytes, err := os.ReadFile(failureFiles[0])
		Expect(err).NotTo(HaveOccurred())
		failureContent := string(failureBytes)

		Expect(failureContent).To(ContainSubstring("apiVersion: route.openshift.io/v1"))
		Expect(failureContent).To(ContainSubstring("kind: Route"))
		Expect(failureContent).To(ContainSubstring("status: Incompatible"))
	})
})
