package framework

import (
	"fmt"
	"log"
	"strings"
)

type Resource interface {
	Delete(k KubectlRunner) error
	Create(k KubectlRunner) error
}

func ResourceCleanup(clusters []KubectlRunner, resources []Resource) {
	for _, k := range clusters {
		for _, r := range resources {
			if err := r.Delete(k); err != nil {
				log.Printf("cleanup: %v", err)
			}
		}
	}
}

type ClusterRole struct {
	Name       string
	Permission string
	Label      string
}

func (cr ClusterRole) Create(k KubectlRunner) error {
	var verbs string
	switch cr.Permission {
	case "write":
		verbs = "get,list,watch,create,update,delete"
	case "patch":
		verbs = "get,list,watch,patch"
	default:
		verbs = "get,list,watch"
	}
	_, err := k.Run("create", "clusterrole", cr.Name, "--verb="+verbs, "--resource=pods")
	if err != nil {
		log.Printf("failed to create ClusterRole %s: %v", cr.Name, err)
		return err
	}
	log.Printf("created %s ClusterRole %s", cr.Permission, cr.Name)
	if cr.Label != "" {
		_, err = k.Run("label", "clusterrole", cr.Name, cr.Label)
		if err != nil {
			return fmt.Errorf("failed to label ClusterRole %s: %w", cr.Name, err)
		}
	}
	return nil
}

func (cr ClusterRole) Delete(k KubectlRunner) error {
	_, err := k.Run("delete", "clusterrole", cr.Name, "--ignore-not-found=true")
	if err != nil {
		return fmt.Errorf("failed to delete ClusterRole %s: %w", cr.Name, err)
	}
	return nil
}

type ClusterRoleBinding struct {
	Name            string
	ClusterRoleName string
	Subject         string
	Label           string
}

func (crb ClusterRoleBinding) Create(k KubectlRunner) error {
	_, err := k.Run("create", "clusterrolebinding", crb.Name, "--clusterrole="+crb.ClusterRoleName, crb.Subject)
	if err != nil {
		log.Printf("failed to create ClusterRoleBinding %s: %v", crb.Name, err)
		return err
	}
	log.Printf("created ClusterRoleBinding %s -> ClusterRole %s", crb.Name, crb.ClusterRoleName)
	if crb.Label != "" {
		_, err = k.Run("label", "clusterrolebinding", crb.Name, crb.Label)
		if err != nil {
			return fmt.Errorf("failed to label ClusterRoleBinding %s: %w", crb.Name, err)
		}
	}
	return nil
}

func (crb ClusterRoleBinding) Delete(k KubectlRunner) error {
	_, err := k.Run("delete", "clusterrolebinding", crb.Name, "--ignore-not-found=true")
	if err != nil {
		return fmt.Errorf("failed to delete ClusterRoleBinding %s: %w", crb.Name, err)
	}
	return nil
}

type ServiceAccount struct {
	Name      string
	Namespace string
	Label     string
}

func (sa ServiceAccount) Create(k KubectlRunner) error {
	_, err := k.Run("create", "serviceaccount", sa.Name, "-n", sa.Namespace)
	if err != nil {
		log.Printf("failed to create ServiceAccount %s in %s: %v", sa.Name, sa.Namespace, err)
		return err
	}
	log.Printf("created ServiceAccount %s in %s", sa.Name, sa.Namespace)
	if sa.Label != "" {
		_, err = k.Run("label", "serviceaccount", sa.Name, "-n", sa.Namespace, sa.Label)
		if err != nil {
			return fmt.Errorf("failed to label ServiceAccount %s: %w", sa.Name, err)
		}
	}
	return nil
}

func (sa ServiceAccount) Delete(k KubectlRunner) error {
	_, err := k.Run("delete", "serviceaccount", sa.Name, "-n", sa.Namespace, "--ignore-not-found=true")
	if err != nil {
		return fmt.Errorf("failed to delete ServiceAccount %s: %w", sa.Name, err)
	}
	return nil
}

type CustomResourceDefinition struct {
	Name string
	YAML string
}

func (crd CustomResourceDefinition) Create(k KubectlRunner) error {
	_, err := k.RunWithStdin(crd.YAML, "apply", "-f", "-")
	if err != nil {
		log.Printf("failed to create CRD %s: %v", crd.Name, err)
	} else {
		log.Printf("created CRD %s", crd.Name)
	}
	return err
}

func (crd CustomResourceDefinition) Delete(k KubectlRunner) error {
	_, err := k.Run("delete", "crd", crd.Name, "--ignore-not-found=true")
	if err != nil {
		return fmt.Errorf("failed to delete CRD %s: %w", crd.Name, err)
	}
	return nil
}

func (crd CustomResourceDefinition) WaitForEstablished(k KubectlRunner) error {
	_, err := k.Run("wait", "--for=condition=Established", "crd/"+crd.Name, "--timeout=30s")
	if err != nil {
		return fmt.Errorf("CRD %s not established: %w", crd.Name, err)
	}
	log.Printf("CRD %s is Established", crd.Name)
	return nil
}

type CustomResource struct {
	Name      string
	Namespace string
	Kind      string
	YAML      string
}

func (cr CustomResource) Create(k KubectlRunner) error {
	_, err := k.RunWithStdin(cr.YAML, "apply", "-f", "-", "-n", cr.Namespace)
	if err != nil {
		log.Printf("failed to create %s %s: %v", cr.Kind, cr.Name, err)
	} else {
		log.Printf("created %s %s in %s", cr.Kind, cr.Name, cr.Namespace)
	}
	return err
}

func (cr CustomResource) AssertField(k KubectlRunner, jsonpath, expected string) error {
	val, err := k.Run("get", strings.ToLower(cr.Kind), cr.Name, "-n", cr.Namespace, "-o", "jsonpath="+jsonpath)
	if err != nil {
		return fmt.Errorf("failed to get %s %s field %s: %w", cr.Kind, cr.Name, jsonpath, err)
	}
	if val != expected {
		return fmt.Errorf("%s %s field %s: expected %q, got %q", cr.Kind, cr.Name, jsonpath, expected, val)
	}
	return nil
}

func (cr CustomResource) Delete(k KubectlRunner) error {
	_, err := k.Run("delete", strings.ToLower(cr.Kind), cr.Name, "-n", cr.Namespace, "--ignore-not-found=true")
	if err != nil {
		return fmt.Errorf("failed to delete %s %s: %w", cr.Kind, cr.Name, err)
	}
	return nil
}

type NameSpace struct {
	Name       string
	Permission string
	Label      string
}

func (n NameSpace) Delete(k KubectlRunner) {
	if _, err := k.Run("delete", "namespace", n.Name, "--ignore-not-found=true", "--wait=true"); err != nil {
		log.Printf("cleanup: failed to delete namespace %q: %v", n.Name, err)
	}
}
func (n NameSpace) Create(k KubectlRunner) {
	log.Printf("Created")

}
