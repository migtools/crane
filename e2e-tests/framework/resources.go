package framework

import (
	"errors"
	"fmt"
	"log"
	"strings"
)

type Resource interface {
	Delete(k KubectlRunner) error
	Create(k KubectlRunner) error
}

func ResourceCleanup(clusters []KubectlRunner, resources []Resource) error {
	var errs []error
	for _, k := range clusters {
		for _, r := range resources {
			if err := r.Delete(k); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
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
		return fmt.Errorf("failed to create ClusterRole %s: %w", cr.Name, err)
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

type ClusterRoleBinding struct {
	Name            string
	ClusterRoleName string
	Label           string
}

func (crb ClusterRoleBinding) Create(k KubectlRunner) error {
	_, err := k.Run("create", "clusterrolebinding", crb.Name, "--clusterrole="+crb.ClusterRoleName)
	if err != nil {
		return fmt.Errorf("failed to create ClusterRoleBinding %s: %w", crb.Name, err)
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

func (crb ClusterRoleBinding) AddSubject(k KubectlRunner, sa ServiceAccount) error {
	subject := fmt.Sprintf(`{"kind":"ServiceAccount","name":"%s","namespace":"%s"}`, sa.Name, sa.Namespace)

	out, err := k.Run("get", "clusterrolebinding", crb.Name, "-o", "jsonpath={.subjects}")
	if err != nil {
		return fmt.Errorf("failed to check subjects on CRB %s: %w", crb.Name, err)
	}

	var patch string
	if out == "" {
		patch = fmt.Sprintf(`[{"op":"add","path":"/subjects","value":[%s]}]`, subject)
	} else {
		patch = fmt.Sprintf(`[{"op":"add","path":"/subjects/-","value":%s}]`, subject)
	}

	_, err = k.Run("patch", "clusterrolebinding", crb.Name, "--type=json", "-p", patch)
	if err != nil {
		return fmt.Errorf("failed to add subject to CRB %s: %w", crb.Name, err)
	}
	return nil
}

type ServiceAccount struct {
	Name      string
	Namespace string
}

func (sa ServiceAccount) Create(k KubectlRunner) error {
	_, err := k.Run("create", "serviceaccount", sa.Name, "-n", sa.Namespace)
	if err != nil {
		return fmt.Errorf("failed to create ServiceAccount %s in %s: %w", sa.Name, sa.Namespace, err)
	}
	log.Printf("created ServiceAccount %s in %s", sa.Name, sa.Namespace)
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
		return fmt.Errorf("failed to create CRD %s: %w", crd.Name, err)
	}
	log.Printf("created CRD %s", crd.Name)
	return nil
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
	Resource  string
	YAML      string
}

func (cr CustomResource) Create(k KubectlRunner) error {
	_, err := k.RunWithStdin(cr.YAML, "apply", "-f", "-", "-n", cr.Namespace)
	if err != nil {
		return fmt.Errorf("failed to create %s %s: %w", cr.Kind, cr.Name, err)
	}
	log.Printf("created %s %s in %s", cr.Kind, cr.Name, cr.Namespace)
	return nil
}

func (cr CustomResource) Delete(k KubectlRunner) error {
	if cr.Resource == "" {
		return fmt.Errorf("failed to delete %s %s: missing API resource name for kind %s", cr.Kind, cr.Name, cr.Kind)
	}
	_, err := k.Run("delete", cr.Resource, cr.Name, "-n", cr.Namespace, "--ignore-not-found=true")
	if err != nil {
		return fmt.Errorf("failed to delete %s %s (api resource %s): %w", cr.Kind, cr.Name, cr.Resource, err)
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
	_, err := k.Run("delete", "namespace", n.Name, "--ignore-not-found=true", "--wait=true", "--timeout=60s")
	if err != nil {
		return fmt.Errorf("failed to delete namespace %s: %w", n.Name, err)
	}
	return nil
}
