package framework

import (
	"fmt"
	"log"
	"strings"

	"github.com/konveyor/crane/e2e-tests/utils"
	"github.com/onsi/gomega"
)

const (
	defaultQuiesceTimeout      = "90s"
	defaultQuiescePollInterval = "3s"
)

// RunCranePipeline executes export, transform, and apply in sequence.
func RunCranePipeline(runner CraneRunner, namespace, exportDir, transformDir, outputDir string) error {
	if err := runner.Export(namespace, exportDir); err != nil {
		return err
	}
	if err := runner.Transform(exportDir, transformDir); err != nil {
		return err
	}
	if err := runner.Apply(exportDir, transformDir, outputDir); err != nil {
		return err
	}
	return nil
}

// RunCranePipelineWithChecks runs the pipeline and verifies generated stage files.
func RunCranePipelineWithChecks(runner CraneRunner, namespace string, paths ScenarioPaths) error {
	if err := RunCranePipeline(runner, namespace, paths.ExportDir, paths.TransformDir, paths.OutputDir); err != nil {
		return err
	}

	if err := checkAndLogStageFiles("export", paths.ExportDir); err != nil {
		return err
	}
	if err := checkAndLogStageFiles("transform", paths.TransformDir); err != nil {
		return err
	}
	if err := checkAndLogStageFiles("output", paths.OutputDir); err != nil {
		return err
	}
	return nil
}

// PrepareSourceApp deploys, validates, and scales down the source application.
func PrepareSourceApp(srcApp K8sDeployApp, kubectlSrc KubectlRunner) error {
	if err := srcApp.Deploy(); err != nil {
		return err
	}
	if err := srcApp.Validate(); err != nil {
		return err
	}
	if err := kubectlSrc.ScaleDeploymentIfPresent(srcApp.Namespace, srcApp.Name, 0); err != nil {
		return err
	}
	return nil
}

// WaitForSourceQuiesce waits until source pods and service endpoints drain.
// It is intended to be called before running export in migration E2E tests.
func WaitForSourceQuiesce(kubectl KubectlRunner, namespace, podSelector, serviceName string) {
	log.Printf(
		"Waiting for source quiesce in namespace %s (pod selector=%s, service=%s)",
		namespace, podSelector, serviceName,
	)

	gomega.Eventually(func() (string, error) {
		out, err := kubectl.Run(
			"get", "pods",
			"--namespace", namespace,
			"-l", podSelector,
			"-o", "name",
		)
		if err != nil {
			return "", err
		}
		return StripKubectlWarnings(out), nil
	}, defaultQuiesceTimeout, defaultQuiescePollInterval).Should(gomega.BeEmpty())

	gomega.Eventually(func() (string, error) {
		out, err := kubectl.Run(
			"get", "endpointslice",
			"--namespace", namespace,
			"-l", "kubernetes.io/service-name="+serviceName,
			"-o", "jsonpath={range .items[*].endpoints[*]}x{end}",
		)
		if err != nil {
			return "", err
		}
		return StripKubectlWarnings(out), nil
	}, defaultQuiesceTimeout, defaultQuiescePollInterval).Should(gomega.BeEmpty())

	gomega.Eventually(func() (string, error) {
		out, err := kubectl.Run(
			"get", "endpoints", serviceName,
			"--namespace", namespace,
			"-o", "jsonpath={range .subsets[*].addresses[*]}x{end}",
		)
		if err != nil {
			if strings.Contains(err.Error(), "NotFound") {
				return "", nil
			}
			return "", err
		}
		return StripKubectlWarnings(out), nil
	}, defaultQuiesceTimeout, defaultQuiescePollInterval).Should(gomega.BeEmpty())
}

// PrepareSourceAppNoQuiesce deploys and validates the source application without scaling it down.
func PrepareSourceAppNoQuiesce(srcApp K8sDeployApp) error {
	if err := srcApp.Deploy(); err != nil {
		return err
	}
	if err := srcApp.Validate(); err != nil {
		return err
	}
	return nil
}

// ApplyOutputToTarget creates namespace, validates, and applies rendered manifests.
func ApplyOutputToTarget(kubectlTgt KubectlRunner, namespace string, outputDir string) error {
	if err := kubectlTgt.CreateNamespace(namespace); err != nil {
		return err
	}
	return applyOutputManifests(kubectlTgt, outputDir)
}

// ApplyOutputToTargetNonAdmin validates and applies rendered manifests without creating namespace.
func ApplyOutputToTargetNonAdmin(kubectlTgt KubectlRunner, outputDir string) error {
	return applyOutputManifests(kubectlTgt, outputDir)
}

func applyOutputManifests(kubectlTgt KubectlRunner, outputDir string) error {
	if err := kubectlTgt.ValidateApplyDir(outputDir); err != nil {
		return err
	}
	if err := kubectlTgt.ApplyDir(outputDir); err != nil {
		return err
	}
	return nil
}

// checkAndLogStageFiles validates stage output exists and logs the file list.
func checkAndLogStageFiles(stage, dir string) error {
	hasFiles, files, err := utils.HasFilesRecursively(dir)
	if err != nil {
		return err
	}
	if !hasFiles {
		return fmt.Errorf("expected crane %s to produce files in %s", stage, dir)
	}
	log.Printf("%s files:\n%s\n", stage, files)
	return nil
}
