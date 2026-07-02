package framework

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/konveyor/crane/e2e-tests/config"
	"github.com/konveyor/crane/e2e-tests/utils"
)

type MigrationScenario struct {
	AppName            string
	Namespace          string
	SrcApp             K8sDeployApp
	TgtApp             K8sDeployApp
	SrcAppNonAdmin     K8sDeployApp
	TgtAppNonAdmin     K8sDeployApp
	KubectlSrc         KubectlRunner
	KubectlTgt         KubectlRunner
	KubectlSrcNonAdmin KubectlRunner
	KubectlTgtNonAdmin KubectlRunner
	Crane              CraneRunner
	CraneNonAdmin      CraneRunner
}

// NewMigrationScenario builds shared runners and app objects for a migration test.
// When config.RunAs == "admin", the NonAdmin fields are populated with admin contexts
// so all test code using those fields automatically runs with cluster-admin credentials.
func NewMigrationScenario(appName, namespace, k8sDeployBin, craneBin, srcCtx, tgtCtx string) MigrationScenario {
	srcNonAdminCtx := config.SourceNonAdminContext
	tgtNonAdminCtx := config.TargetNonAdminContext
	if config.RunAs == "admin" {
		srcNonAdminCtx = srcCtx
		tgtNonAdminCtx = tgtCtx
	}

	return MigrationScenario{
		AppName:   appName,
		Namespace: namespace,
		SrcApp: K8sDeployApp{
			Name:            appName,
			Namespace:       namespace,
			Bin:             k8sDeployBin,
			Context:         srcCtx,
			InsecureSkipTLS: config.InsecureSkipTLSVerify,
		},
		TgtApp: K8sDeployApp{
			Name:            appName,
			Namespace:       namespace,
			Bin:             k8sDeployBin,
			Context:         tgtCtx,
			InsecureSkipTLS: config.InsecureSkipTLSVerify,
		},
		SrcAppNonAdmin: K8sDeployApp{
			Name:            appName,
			Namespace:       namespace,
			Bin:             k8sDeployBin,
			Context:         srcNonAdminCtx,
			InsecureSkipTLS: config.InsecureSkipTLSVerify,
		},
		TgtAppNonAdmin: K8sDeployApp{
			Name:            appName,
			Namespace:       namespace,
			Bin:             k8sDeployBin,
			Context:         tgtNonAdminCtx,
			InsecureSkipTLS: config.InsecureSkipTLSVerify,
		},
		KubectlSrc:         KubectlRunner{Bin: "kubectl", Context: srcCtx},
		KubectlTgt:         KubectlRunner{Bin: "kubectl", Context: tgtCtx},
		KubectlSrcNonAdmin: KubectlRunner{Bin: "kubectl", Context: srcNonAdminCtx},
		KubectlTgtNonAdmin: KubectlRunner{Bin: "kubectl", Context: tgtNonAdminCtx},
		Crane: CraneRunner{
			Bin:           craneBin,
			SourceContext: srcCtx,
		},
		CraneNonAdmin: CraneRunner{
			Bin:           craneBin,
			SourceContext: srcNonAdminCtx,
		},
	}
}

type ScenarioPaths struct {
	TempDir      string
	ExportDir    string
	TransformDir string
	OutputDir    string
	ValidateDir  string
}

// NewScenarioPaths creates a temp workspace and standard export/transform/output dirs.
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
		ValidateDir:  tempDir + "/validate",
	}, nil
}

// CleanupScenario removes temp artifacts and cleans source and target test apps.
// It runs all steps even if earlier ones fail (best effort) and returns a joined
// error so callers can log or assert; each failure is also logged immediately.
func CleanupScenario(tempDir string, srcApp, tgtApp K8sDeployApp) error {
	log.Println("Starting cleanup...")
	var errs []error

	if tempDir != "" {
		log.Printf("Removing temp dir: %s\n", tempDir)
		if err := os.RemoveAll(tempDir); err != nil {
			log.Printf("cleanup: failed to remove temp dir %q: %v", tempDir, err)
			errs = append(errs, fmt.Errorf("remove temp dir %q: %w", tempDir, err))
		}
	}

	log.Printf("Cleaning source app: %s/%s\n", srcApp.Namespace, srcApp.Name)
	if err := srcApp.Cleanup(); err != nil {
		log.Printf("cleanup: failed to remove source app %s/%s: %v", srcApp.Namespace, srcApp.Name, err)
		errs = append(errs, fmt.Errorf("source app %s/%s: %w", srcApp.Namespace, srcApp.Name, err))
	}

	log.Printf("Cleaning target app: %s/%s\n", tgtApp.Namespace, tgtApp.Name)
	if err := tgtApp.Cleanup(); err != nil {
		log.Printf("cleanup: failed to remove target app %s/%s: %v", tgtApp.Namespace, tgtApp.Name, err)
		errs = append(errs, fmt.Errorf("target app %s/%s: %w", tgtApp.Namespace, tgtApp.Name, err))
	}

	if len(errs) > 0 {
		log.Printf("Cleanup finished with %d error(s); resources or temp files may remain", len(errs))
		return errors.Join(errs...)
	}
	log.Println("Cleanup completed.")
	return nil
}