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
		Bin:     adminKubectl.Bin,
		Context: nonAdminContext,
	}

	can, err := userKubectl.CanI("create", "namespace", "")
	if err != nil {
		if revokeErr := adminKubectl.RevokeNamespaceAdminFromUser(namespace, username); revokeErr != nil {
			log.Printf("failed to rollback namespace admin grant for user %q in namespace %q after preflight failure: %v", username, namespace, revokeErr)
		}
		return KubectlRunner{}, nil, fmt.Errorf(
			"failed RBAC preflight for context %q (user %q): cannot evaluate cluster-scope permission create namespaces: %w",
			nonAdminContext, username, err,
		)
	}
	if can {
		if revokeErr := adminKubectl.RevokeNamespaceAdminFromUser(namespace, username); revokeErr != nil {
			log.Printf("failed to rollback namespace admin grant for user %q in namespace %q after preflight failure: %v", username, namespace, revokeErr)
		}
		return KubectlRunner{}, nil, fmt.Errorf(
			"RBAC preflight failed for context %q (user %q): expected non-admin, but user can create namespaces (cluster-scope)",
			nonAdminContext, username,
		)
	}

	cleanup := func() {
		if err := adminKubectl.RevokeNamespaceAdminFromUser(namespace, username); err != nil {
			log.Printf("failed to revoke namespace admin from user %q in namespace %q: %v", username, namespace, err)
		}
	}

	return userKubectl, cleanup, nil
}

// SetupNamespaceAdminUsersForScenario grants namespace-scoped admin permissions
// on both source and target clusters for the configured non-admin contexts.
// It returns kubectl runners bound to the non-admin contexts and a combined
// cleanup callback that revokes both bindings.
func SetupNamespaceAdminUsersForScenario(scenario MigrationScenario, namespace string) (KubectlRunner, KubectlRunner, func(), error) {
	srcNonAdminKubectl, srcCleanup, err := SetupNamespaceAdminUser(
		scenario.KubectlSrc,
		scenario.KubectlSrcNonAdmin.Context,
		namespace,
	)
	if err != nil {
		return KubectlRunner{}, KubectlRunner{}, nil, err
	}

	tgtNonAdminKubectl, tgtCleanup, err := SetupNamespaceAdminUser(
		scenario.KubectlTgt,
		scenario.KubectlTgtNonAdmin.Context,
		namespace,
	)
	if err != nil {
		srcCleanup()
		return KubectlRunner{}, KubectlRunner{}, nil, err
	}

	cleanup := func() {
		// Revoke in reverse order of creation.
		tgtCleanup()
		srcCleanup()
	}

	return srcNonAdminKubectl, tgtNonAdminKubectl, cleanup, nil
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
