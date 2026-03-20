package framework

import (
	"log"
	"os"

	"github.com/konveyor/crane/e2e/utils"
)

type MigrationScenario struct {
	AppName    string
	Namespace  string
	SrcApp     K8sDeployApp
	TgtApp     K8sDeployApp
	KubectlSrc KubectlRunner
	KubectlTgt KubectlRunner
	Crane      CraneRunner
}

func NewMigrationScenario(appName, namespace, k8sDeployBin, craneBin, srcCtx, tgtCtx string) MigrationScenario {
	return MigrationScenario{
		AppName:   appName,
		Namespace: namespace,
		SrcApp: K8sDeployApp{
			Name:      appName,
			Namespace: namespace,
			Bin:       k8sDeployBin,
			Context:   srcCtx,
		},
		TgtApp: K8sDeployApp{
			Name:      appName,
			Namespace: namespace,
			Bin:       k8sDeployBin,
			Context:   tgtCtx,
		},
		KubectlSrc: KubectlRunner{Bin: "kubectl", Context: srcCtx},
		KubectlTgt: KubectlRunner{Bin: "kubectl", Context: tgtCtx},
		Crane: CraneRunner{
			Bin:           craneBin,
			SourceContext: srcCtx,
		},
	}
}

type ScenarioPaths struct {
	TempDir      string
	ExportDir    string
	TransformDir string
	OutputDir    string
}

func NewScenarioPaths(prefix string) (ScenarioPaths, error) {
	tempDir, err := utils.CreateTempDir(prefix)
	if err != nil {
		return ScenarioPaths{}, err
	}

	return ScenarioPaths{
		TempDir:      tempDir,
		ExportDir:    tempDir + "/export",
		TransformDir: tempDir + "/transform",
		OutputDir:    tempDir + "/output",
	}, nil
}

func CleanupScenario(tempDir string, srcApp, tgtApp K8sDeployApp) {
	log.Println("Starting cleanup...")

	log.Printf("Removing temp dir: %s\n", tempDir)
	_ = os.RemoveAll(tempDir)

	log.Printf("Cleaning source app: %s/%s\n", srcApp.Namespace, srcApp.Name)
	_ = srcApp.Cleanup()

	log.Printf("Cleaning target app: %s/%s\n", tgtApp.Namespace, tgtApp.Name)
	_ = tgtApp.Cleanup()

	log.Println("Cleanup completed.")
}
