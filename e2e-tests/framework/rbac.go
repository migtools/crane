package framework

import (
	"fmt"
	"log"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
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

// SetupActiveNamespaceAdmin sets up namespace-scoped access on a single cluster.
// When config.RunAs == "admin", it creates the namespace with the admin runner and returns
// a no-op cleanup. Otherwise it delegates to SetupNamespaceAdminUser.
func SetupActiveNamespaceAdmin(adminKubectl KubectlRunner, nonAdminContext, namespace string) (KubectlRunner, func(), error) {
	if config.RunAs == "admin" {
		if err := adminKubectl.CreateNamespace(namespace); err != nil {
			return KubectlRunner{}, nil, fmt.Errorf("failed to create namespace %q: %w", namespace, err)
		}
		return adminKubectl, func() {}, nil
	}
	return SetupNamespaceAdminUser(adminKubectl, nonAdminContext, namespace)
}

// SetupActiveKubectlRunners returns kubectl runners appropriate for the current RunAs mode.
// When config.RunAs == "admin", it creates the namespace with admin credentials and returns
// admin runners directly (bypassing RBAC setup). Otherwise it delegates to
// SetupNamespaceAdminUsersForScenario to grant namespace-scoped permissions to the non-admin user.
func SetupActiveKubectlRunners(scenario MigrationScenario, namespace string) (KubectlRunner, KubectlRunner, func(), error) {
	if config.RunAs == "admin" {
		if srcUser, err := ResolveUsernameForContext(scenario.KubectlSrc.Context); err != nil {
			log.Printf("[run-as=admin] source context=%q: could not resolve username: %v", scenario.KubectlSrc.Context, err)
		} else {
			log.Printf("[run-as=admin] source context=%q running as user=%q", scenario.KubectlSrc.Context, srcUser)
		}
		if tgtUser, err := ResolveUsernameForContext(scenario.KubectlTgt.Context); err != nil {
			log.Printf("[run-as=admin] target context=%q: could not resolve username: %v", scenario.KubectlTgt.Context, err)
		} else {
			log.Printf("[run-as=admin] target context=%q running as user=%q", scenario.KubectlTgt.Context, tgtUser)
		}
		if err := scenario.KubectlSrc.CreateNamespace(namespace); err != nil {
			return KubectlRunner{}, KubectlRunner{}, nil, fmt.Errorf("failed to create namespace %q on source: %w", namespace, err)
		}
		if err := scenario.KubectlTgt.CreateNamespace(namespace); err != nil {
			return KubectlRunner{}, KubectlRunner{}, nil, fmt.Errorf("failed to create namespace %q on target: %w", namespace, err)
		}
		return scenario.KubectlSrc, scenario.KubectlTgt, func() {}, nil
	}
	return SetupNamespaceAdminUsersForScenario(scenario, namespace)
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
