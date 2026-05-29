package framework

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/onsi/gomega"
)

type ClusterResource struct {
	Kind string
	Name string
}

// ClusterResourceCleanup deletes cluster-scoped resources (e.g. ClusterRoles, CRBs) on both clusters. Best-effort.
func ClusterResourceCleanup(srcKubectl, tgtKubectl KubectlRunner, resources []ClusterResource) {
	for _, k := range []KubectlRunner{srcKubectl, tgtKubectl} {
		for _, r := range resources {
			if _, err := k.Run("delete", r.Kind, r.Name, "--ignore-not-found=true"); err != nil {
				log.Printf("cleanup: failed to delete %s %s: %v", r.Kind, r.Name, err)
			}
		}
	}
}

// ScenarioCleanup removes temp dirs, apps, and namespaces on both clusters. Best-effort.
func ScenarioCleanup(paths ScenarioPaths, srcApp, tgtApp K8sDeployApp, srcKubectl, tgtKubectl KubectlRunner, namespace string) {
	if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
		log.Printf("cleanup: %v", err)
	}
	for _, k := range []KubectlRunner{srcKubectl, tgtKubectl} {
		if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
			log.Printf("cleanup: failed to delete namespace %q: %v", namespace, err)
		}
	}
}

// ValidateDirResources checks that a directory exists and contains files matching the given glob patterns.
func ValidateDirResources(path string, resources []string) error {
	_, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("directory not found at %s: %w", path, err)
	}

	for _, resource := range resources {
		matches, err := filepath.Glob(filepath.Join(path, resource))
		if err != nil {
			return fmt.Errorf("glob error for %s at %s: %w", resource, path, err)
		}
		if len(matches) == 0 {
			return fmt.Errorf("no files matching %s at %s", resource, path)
		}
		log.Printf("found %d file(s) matching %s at %s", len(matches), resource, path)
	}
	return nil
}

// ValidatePipelineClusterResources verifies cluster resource files exist across all pipeline stages (export, transform, apply).
// Pass nil for transformStages to default to 10_KubernetesPlugin.
func ValidatePipelineClusterResources(paths ScenarioPaths, namespace string, resources []string, transformStages *[]string) error {
	exportClusterDir := filepath.Join(paths.ExportDir, "resources", namespace, "_cluster")
	log.Printf("Validating Export _cluster directory")
	if err := ValidateDirResources(exportClusterDir, resources); err != nil {
		return fmt.Errorf("Export: %w", err)
	}

	stages := []string{"10_KubernetesPlugin"}
	if transformStages != nil && len(*transformStages) > 0 {
		stages = *transformStages
	}
	for _, stage := range stages {
		transformClusterDir := filepath.Join(paths.TransformDir, ".work", stage, "output", "_cluster")
		log.Printf("Validating Transform(%s) _cluster directory", stage)
		if err := ValidateDirResources(transformClusterDir, resources); err != nil {
			return fmt.Errorf("Transform(%s): %w", stage, err)
		}
	}

	outputClusterDir := filepath.Join(paths.OutputDir, "resources", "_cluster")
	log.Printf("Validating Apply _cluster directory")
	if err := ValidateDirResources(outputClusterDir, resources); err != nil {
		return fmt.Errorf("Apply: %w", err)
	}

	return nil
}

// RunPipeline sets the work dir and runs crane export, transform, and apply.
func RunPipeline(runner *CraneRunner, namespace string, paths ScenarioPaths) error {
	runner.WorkDir = paths.TempDir
	log.Printf("Running crane pipeline for namespace %s", namespace)
	if err := RunCranePipelineWithChecks(*runner, namespace, paths); err != nil {
		return fmt.Errorf("pipeline failed: %w", err)
	}
	log.Printf("Crane pipeline completed")
	return nil
}

// ScaleAndValidateTargetApp scales the deployment to n replica and waits up to 2m for the app to validate.
func ScaleAndValidateTargetApp(kubectlTgt KubectlRunner, tgtApp K8sDeployApp, namespace, appName string) {
	gomega.Expect(kubectlTgt.ScaleDeployment(namespace, appName, 1)).NotTo(gomega.HaveOccurred())
	gomega.Eventually(tgtApp.Validate, "2m", "10s").Should(gomega.Succeed())
	log.Printf("Target app validated successfully")
}

type ExpectedClusterRoleBinding struct {
	ClusterRoleBindingName string
	ClusterRoleName        string
	SubjectName            string
}

// ValidateClusterRBAC verifies that each CRB exists, references the expected ClusterRole, and has the expected subject.
func ValidateClusterRBAC(kubectl KubectlRunner, namespace string, bindings []ExpectedClusterRoleBinding) error {
	clusterRoles := map[string]bool{}
	for _, b := range bindings {
		clusterRoles[b.ClusterRoleName] = true
	}
	for cr := range clusterRoles {
		if _, err := kubectl.Run("get", "clusterrole", cr); err != nil {
			return fmt.Errorf("ClusterRole %s not found: %w", cr, err)
		}
		log.Printf("ClusterRole %s exists", cr)
	}

	for _, b := range bindings {
		if _, err := kubectl.Run("get", "clusterrolebinding", b.ClusterRoleBindingName); err != nil {
			return fmt.Errorf("ClusterRoleBinding %s not found: %w", b.ClusterRoleBindingName, err)
		}

		roleRef, err := kubectl.Run("get", "clusterrolebinding", b.ClusterRoleBindingName, "-o", "jsonpath={.roleRef.name}")
		if err != nil {
			return fmt.Errorf("failed to get roleRef for CRB %s: %w", b.ClusterRoleBindingName, err)
		}
		if roleRef != b.ClusterRoleName {
			return fmt.Errorf("CRB %s references %s, expected %s", b.ClusterRoleBindingName, roleRef, b.ClusterRoleName)
		}

		subject, err := kubectl.Run("get", "clusterrolebinding", b.ClusterRoleBindingName, "-o", "jsonpath={.subjects[0].name}")
		if err != nil {
			return fmt.Errorf("failed to get subject for CRB %s: %w", b.ClusterRoleBindingName, err)
		}
		if subject != b.SubjectName {
			return fmt.Errorf("CRB %s subject is %s, expected %s", b.ClusterRoleBindingName, subject, b.SubjectName)
		}
		log.Printf("CRB %s -> CR %s (subject: %s) verified", b.ClusterRoleBindingName, b.ClusterRoleName, b.SubjectName)
	}
	return nil
}

// AssertNoExportFailures returns an error if any files exist in the export failures directory.
func AssertNoExportFailures(exportDir, namespace string) error {
	failuresDir := filepath.Join(exportDir, "failures", namespace)
	files, err := filepath.Glob(filepath.Join(failuresDir, "*"))
	if err != nil {
		return fmt.Errorf("glob error checking export failures: %w", err)
	}
	if len(files) > 0 {
		return fmt.Errorf("found %d export failure(s) in %s", len(files), failuresDir)
	}
	log.Printf("No export failures found in %s", failuresDir)
	return nil
}

func AssertNoClusterResources(basePath string) error {
	_, err := os.Stat(basePath)
	if err != nil {
		log.Printf("directory does not exist at %s (no cluster resources possible)", basePath)
		return nil
	}

	var found []string
	filepath.WalkDir(basePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if strings.HasPrefix(d.Name(), "Cluster") {
			found = append(found, path)
		}
		return nil
	})

	if len(found) > 0 {
		return fmt.Errorf("found %d cluster resource(s) under %s: %v", len(found), basePath, found)
	}
	log.Printf("No cluster resources found under %s", basePath)
	return nil
}

func CrateCrAndValidate(kubectl KubectlRunner, prem string, crName string) error {
	var verbs string
	switch prem {
	case "write":
		verbs = "get,list,watch,create,update,delete"
	case "patch":
		verbs = "get,list,watch,patch"
	default:
		verbs = "get,list,watch"
	}

	_, crErr := kubectl.Run("create", "clusterrole", crName, "--verb="+verbs, "--resource=pods")
	if crErr == nil {
		log.Printf("Created %s ClusterRole %s", prem, crName)
	} else {
		log.Printf("Failed To Created ClusterRole %s", crName)
	}
	return crErr
}

func CrateAndValidateCrb(kubectl KubectlRunner, namespace string, clusterRoleBindingName string, clusterRoleName string, serviceAccount *string) error {
	var saName string
	if serviceAccount == nil {
		saName = "default"
	} else {
		saName = *serviceAccount
	}

	_, crbErr := kubectl.Run("create", "clusterrolebinding", clusterRoleBindingName, "--clusterrole="+clusterRoleName,
		"--serviceaccount="+namespace+":"+saName)
	if crbErr == nil {
		log.Printf("Created #1 ClusterRoleBinding %s -> ClusterRole %s (subject: %s:%s)", clusterRoleBindingName, clusterRoleName, namespace, saName)

	} else {
		log.Printf("Failed To Created ClusterRoleBinding %s\n", clusterRoleBindingName)
	}

	return crbErr
}

func CrateAndValidateServiceAccount(kubectl KubectlRunner, saName string, namespace string) error {
	_, err := kubectl.Run("create", "serviceaccount", saName, "-n", namespace)
	if err == nil {
		log.Printf("Serviceaccount %s Created on  %s\n", saName, namespace)
	} else {
		log.Printf("Failed To Created serviceAccount %s", saName)
	}
	return err
}

func NonAdminApplyOutput(kubectlTgt KubectlRunner, path string, namespace string) error {
	outputNamespacedir := filepath.Join(path, "resources", namespace)
	_, err := os.Stat(outputNamespacedir)
	if err != nil {
		log.Printf("failures: %v \n", err)
		return err
	}
	err = kubectlTgt.ApplyDir(outputNamespacedir)
	return err
}

func CreateNamespaceAndDryRun(kubectl KubectlRunner, namespace, outputDir string) error {
	log.Printf("Creating namespace %s on target", namespace)
	if _, err := kubectl.Run("create", "namespace", namespace); err != nil {
		return fmt.Errorf("failed to create namespace %s: %w", namespace, err)
	}
	return kubectl.ValidateApplyDir(outputDir)
}
