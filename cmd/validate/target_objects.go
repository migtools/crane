package validate

import (
	"context"
	"fmt"
	"io"
	"sort"

	internalValidate "github.com/konveyor/crane/internal/validate"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// targetObjectWarnings records non-fatal results from inspecting rendered
// namespaced resources on a live target cluster.
type targetObjectWarnings struct {
	existing []internalValidate.ManifestResource
	unknown  []targetObjectInspectionError
	skipped  []targetObjectInspectionError
}

type targetObjectInspectionError struct {
	resource internalValidate.ManifestResource
	reason   string
}

func (w targetObjectWarnings) hasWarnings() bool {
	return len(w.existing) > 0 || len(w.unknown) > 0 || len(w.skipped) > 0
}

// inspectTargetObjects gets each namespaced rendered object whose API is
// compatible with the target. Object presence is advisory: access failures
// are reported as warnings and do not change compatibility validation.
func inspectTargetObjects(
	ctx context.Context,
	client dynamic.Interface,
	resources []internalValidate.ManifestResource,
	report *internalValidate.ValidationReport,
) targetObjectWarnings {
	resultByIdentity := make(map[string]internalValidate.ValidationResult, len(report.Results))
	for _, result := range report.Results {
		resultByIdentity[validationIdentity(result.APIVersion, result.Kind, result.Namespace)] = result
	}

	warnings := targetObjectWarnings{}
	for _, resource := range resources {
		// This feature intentionally checks only namespaced resources. A
		// namespace-admin normally cannot inspect cluster-scoped objects.
		if resource.Namespace == "" {
			continue
		}

		result, ok := resultByIdentity[validationIdentity(resource.APIVersion, resource.Kind, resource.Namespace)]
		if !ok || result.Status != internalValidate.StatusOK {
			reason := fmt.Sprintf("%s is not served by the target cluster", resource.APIVersion)
			if ok && result.Reason != "" {
				reason = result.Reason
			}
			warnings.skipped = append(warnings.skipped, targetObjectInspectionError{
				resource: resource,
				reason:   reason,
			})
			continue
		}
		if resource.Name == "" {
			warnings.unknown = append(warnings.unknown, targetObjectInspectionError{
				resource: resource,
				reason:   "metadata.name is empty",
			})
			continue
		}

		gvr := schema.GroupVersionResource{Group: resource.Group, Version: resource.Version, Resource: result.ResourcePlural}
		_, err := client.Resource(gvr).Namespace(resource.Namespace).Get(ctx, resource.Name, metav1.GetOptions{})
		switch {
		case err == nil:
			warnings.existing = append(warnings.existing, resource)
		case apierrors.IsNotFound(err):
			// The object is absent. This is the expected no-collision case.
		case apierrors.IsForbidden(err):
			warnings.unknown = append(warnings.unknown, targetObjectInspectionError{resource: resource, reason: err.Error()})
		default:
			warnings.unknown = append(warnings.unknown, targetObjectInspectionError{resource: resource, reason: err.Error()})
		}
	}

	return warnings
}

func validationIdentity(apiVersion, kind, namespace string) string {
	return apiVersion + "\x00" + kind + "\x00" + namespace
}

func formatTargetObjectWarnings(w io.Writer, warnings targetObjectWarnings) {
	if !warnings.hasWarnings() {
		return
	}

	sort.Slice(warnings.existing, func(i, j int) bool {
		return resourceDisplayName(warnings.existing[i]) < resourceDisplayName(warnings.existing[j])
	})
	sort.Slice(warnings.unknown, func(i, j int) bool {
		return resourceDisplayName(warnings.unknown[i].resource) < resourceDisplayName(warnings.unknown[j].resource)
	})
	sort.Slice(warnings.skipped, func(i, j int) bool {
		return resourceDisplayName(warnings.skipped[i].resource) < resourceDisplayName(warnings.skipped[j].resource)
	})

	if len(warnings.existing) > 0 {
		fmt.Fprintf(w, "\nWarning: %d rendered resource(s) already exist on the target cluster.\n", len(warnings.existing))
		fmt.Fprintln(w, "kubectl apply may modify them:")
		for _, resource := range warnings.existing {
			fmt.Fprintf(w, "  - %s\n", resourceDisplayName(resource))
		}
	}
	if len(warnings.unknown) > 0 {
		fmt.Fprintf(w, "\nWarning: %d rendered resource(s) could not be inspected on the target cluster:\n", len(warnings.unknown))
		for _, inspectionErr := range warnings.unknown {
			fmt.Fprintf(w, "  - %s: %s\n", resourceDisplayName(inspectionErr.resource), inspectionErr.reason)
		}
	}
	if len(warnings.skipped) > 0 {
		fmt.Fprintf(w, "\nWarning: target-object presence was not checked for %d rendered resource(s):\n", len(warnings.skipped))
		for _, inspectionErr := range warnings.skipped {
			fmt.Fprintf(w, "  - %s: %s\n", resourceDisplayName(inspectionErr.resource), inspectionErr.reason)
		}
	}
}

func resourceDisplayName(resource internalValidate.ManifestResource) string {
	return fmt.Sprintf("%s/%s/%s", resource.Kind, resource.Namespace, resource.Name)
}
