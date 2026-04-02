package framework

import (
	"fmt"
	"log"
	"strings"

	"github.com/konveyor/crane/e2e-tests/utils"
)

// GrantNamespaceAdminToUser ensures the namespace exists and applies a
// RoleBinding that grants the built-in admin ClusterRole to the provided user
// within that namespace.
func (k KubectlRunner) GrantNamespaceAdminToUser(namespace, username string) error {
	if namespace == "" || username == "" {
		return fmt.Errorf("namespace and username are required")
	}

	err := k.CreateNamespace(namespace)
	if err != nil {
		return fmt.Errorf("failed to create namespace %q: %w", namespace, err)
	}
	rolebindingSpec, err := utils.ReadTestdataFile("rolebinding_namespace_admin.yaml")
	if err != nil {
		return fmt.Errorf("failed to read rolebinding_namespace_admin.yaml: %w", err)
	}
	rolebindingSpec = strings.ReplaceAll(rolebindingSpec, "__USERNAME__", username)
	rolebindingSpec = strings.ReplaceAll(rolebindingSpec, "__NAMESPACE__", namespace)
	if strings.Contains(rolebindingSpec, "__USERNAME__") || strings.Contains(rolebindingSpec, "__NAMESPACE__") {
		return fmt.Errorf("failed to render rolebinding template placeholders for namespace %q and user %q", namespace, username)
	}
	_, err = k.RunWithStdin(rolebindingSpec, "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("failed to apply rolebinding for user %q in namespace %q: %w", username, namespace, err)
	}
	return nil
}

// RevokeNamespaceAdminFromUser deletes the namespace RoleBinding for the given
// user and treats missing bindings as a successful no-op.
func (k KubectlRunner) RevokeNamespaceAdminFromUser(namespace, username string) error {
	if namespace == "" || username == "" {
		return fmt.Errorf("namespace and username are required")
	}

	_, err := k.Run("delete", "rolebinding", username+"-admin", "-n", namespace, "--ignore-not-found=true")
	if err != nil {
		return fmt.Errorf("failed to delete rolebinding for user %q in namespace %q: %w", username, namespace, err)
	}
	return nil
}

// SetupNamespaceAdminUser resolves the username behind a non-admin context,
// grants namespace-scoped admin permissions using an admin runner, and returns
// a non-admin kubectl runner plus a best-effort cleanup function.
func SetupNamespaceAdminUser(adminKubectl KubectlRunner, nonAdminContext, namespace string) (KubectlRunner, func(), error) {
	if nonAdminContext == "" {
		return KubectlRunner{}, nil, fmt.Errorf("non-admin context is required")
	}

	if namespace == "" {
		return KubectlRunner{}, nil, fmt.Errorf("namespace is required")
	}
	if adminKubectl.Bin == "" {
		return KubectlRunner{}, nil, fmt.Errorf("admin kubectl binary is required")
	}

	username, err := ResolveUsernameForContext(nonAdminContext)
	if err != nil {
		return KubectlRunner{}, nil, fmt.Errorf("failed to resolve username for context %q: %w", nonAdminContext, err)
	}

	if err := adminKubectl.GrantNamespaceAdminToUser(namespace, username); err != nil {
		return KubectlRunner{}, nil, fmt.Errorf("failed to grant namespace admin to user %q in namespace %q: %w", username, namespace, err)
	}

	userKubectl := KubectlRunner{
		Bin:     "kubectl",
		Context: nonAdminContext,
	}

	cleanup := func() {
		if err := adminKubectl.RevokeNamespaceAdminFromUser(namespace, username); err != nil {
			log.Printf("failed to revoke namespace admin from user %q in namespace %q: %v", username, namespace, err)
		}
	}

	return userKubectl, cleanup, nil
}
