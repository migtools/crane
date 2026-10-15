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

type targetObject struct {
	entry internalValidate.ManifestEntry
	name  string
}

type targetObjectWarnings struct {
	existing []targetObject
	unknown  []targetObjectInspectionError
}

type targetObjectInspectionError struct {
	object targetObject
	reason string
}

// inspectTargetObjects gets every named, namespaced manifest whose API is
// compatible with the target. Object presence is advisory: access failures do
// not change API compatibility validation.
func inspectTargetObjects(
	ctx context.Context,
	client dynamic.Interface,
	entries []internalValidate.ManifestEntry,
	report *internalValidate.ValidationReport,
) targetObjectWarnings {
	resultByIdentity := make(map[string]internalValidate.ValidationResult, len(report.Results))
	for _, result := range report.Results {
		resultByIdentity[validationIdentity(result.APIVersion, result.Kind, result.Namespace)] = result
	}

	warnings := targetObjectWarnings{}
	for _, entry := range entries {
		// Namespace-admin users normally cannot inspect cluster-scoped objects.
		if entry.Namespace == "" {
			continue
		}

		result, ok := resultByIdentity[validationIdentity(entry.APIVersion, entry.Kind, entry.Namespace)]
		if !ok || result.Status != internalValidate.StatusOK {
			continue
		}

		gvr := schema.GroupVersionResource{Group: entry.Group, Version: entry.Version, Resource: result.ResourcePlural}
		for _, name := range entry.Names {
			object := targetObject{entry: entry, name: name}
			if name == "" {
				warnings.unknown = append(warnings.unknown, targetObjectInspectionError{object: object, reason: "metadata.name is empty"})
				continue
			}

			_, err := client.Resource(gvr).Namespace(entry.Namespace).Get(ctx, name, metav1.GetOptions{})
			switch {
			case err == nil:
				warnings.existing = append(warnings.existing, object)
			case apierrors.IsNotFound(err):
				// The object is absent. This is the expected no-collision case.
			default:
				warnings.unknown = append(warnings.unknown, targetObjectInspectionError{object: object, reason: err.Error()})
			}
		}
	}

	return warnings
}

func validationIdentity(apiVersion, kind, namespace string) string {
	return apiVersion + "\x00" + kind + "\x00" + namespace
}

func formatTargetObjectWarnings(w io.Writer, warnings targetObjectWarnings) {
	if len(warnings.existing) == 0 && len(warnings.unknown) == 0 {
		return
	}

	sort.Slice(warnings.existing, func(i, j int) bool {
		return targetObjectDisplayName(warnings.existing[i]) < targetObjectDisplayName(warnings.existing[j])
	})
	sort.Slice(warnings.unknown, func(i, j int) bool {
		return targetObjectDisplayName(warnings.unknown[i].object) < targetObjectDisplayName(warnings.unknown[j].object)
	})

	if len(warnings.existing) > 0 {
		fmt.Fprintf(w, "\nWarning: %d rendered resource(s) already exist on the target cluster.\n", len(warnings.existing))
		fmt.Fprintln(w, "kubectl apply may modify them:")
		for _, object := range warnings.existing {
			fmt.Fprintf(w, "  - %s\n", targetObjectDisplayName(object))
		}
	}
	if len(warnings.unknown) > 0 {
		fmt.Fprintf(w, "\nWarning: %d rendered resource(s) could not be inspected on the target cluster:\n", len(warnings.unknown))
		for _, inspectionErr := range warnings.unknown {
			fmt.Fprintf(w, "  - %s: %s\n", targetObjectDisplayName(inspectionErr.object), inspectionErr.reason)
		}
	}
}

func targetObjectDisplayName(object targetObject) string {
	return fmt.Sprintf("%s/%s/%s", object.entry.Kind, object.entry.Namespace, object.name)
}
