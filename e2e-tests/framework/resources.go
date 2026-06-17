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
	Name     string
	Verb     string
	Resource string
	Label    string
}

func (cr ClusterRole) Create(k KubectlRunner) error {
	_, err := k.Run("create", "clusterrole", cr.Name, "--verb="+cr.Verb, "--resource="+cr.Resource)
	if err != nil {
		log.Printf("failed to create ClusterRole %s: %v", cr.Name, err)
		return err
	}
	log.Printf("created ClusterRole %s", cr.Name)
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

func (cr CustomResource) Delete(k KubectlRunner) error {
	_, err := k.Run("delete", strings.ToLower(cr.Kind), cr.Name, "-n", cr.Namespace, "--ignore-not-found=true")
	if err != nil {
		return fmt.Errorf("failed to delete %s %s: %w", cr.Kind, cr.Name, err)
	}
	return nil
}

type Namespace struct {
	Name  string
	Label string
}

func (n Namespace) Create(k KubectlRunner) error {
	if err := k.CreateNamespace(n.Name); err != nil {
		return fmt.Errorf("failed to create namespace %s: %w", n.Name, err)
	}
	log.Printf("created namespace %s", n.Name)
	if n.Label != "" {
		_, err := k.Run("label", "namespace", n.Name, n.Label)
		if err != nil {
			return fmt.Errorf("failed to label namespace %s: %w", n.Name, err)
		}
	}
	return nil
}

func (n Namespace) Delete(k KubectlRunner) error {
	_, err := k.Run("delete", "namespace", n.Name, "--ignore-not-found=true", "--wait=true")
	if err != nil {
		return fmt.Errorf("failed to delete namespace %s: %w", n.Name, err)
	}
	return nil
}
