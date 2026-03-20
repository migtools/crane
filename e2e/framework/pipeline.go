package framework

import (
	"fmt"
	"log"

	"github.com/konveyor/crane/e2e/utils"
)

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

func PrepareSourceApp(srcApp K8sDeployApp, kubectlSrc KubectlRunner) error {
	if err := srcApp.Deploy(); err != nil {
		return err
	}
	if err := srcApp.Validate(); err != nil {
		return err
	}
	if err := kubectlSrc.ScaleDeployment(srcApp.Namespace, srcApp.Name, 0); err != nil {
		return err
	}
	return nil
}

func ApplyOutputToTarget(kubectlTgt KubectlRunner, namespace string, outputDir string) error {
	if err := kubectlTgt.CreateNamespace(namespace); err != nil {
		return err
	}
	if err := kubectlTgt.ValidateApplyDir(outputDir); err != nil {
		return err
	}
	if err := kubectlTgt.ApplyDir(outputDir); err != nil {
		return err
	}
	return nil
}

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
