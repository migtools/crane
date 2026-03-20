package framework

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
