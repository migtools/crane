package validate

import "fmt"

// ManifestEntry is one distinct apiVersion+kind+namespace tuple from scanned files.
type ManifestEntry struct {
	APIVersion  string
	Kind        string
	Group       string   // parsed from APIVersion (e.g. "apps" from "apps/v1")
	Version     string   // parsed from APIVersion (e.g. "v1")
	Namespace   string   // from metadata.namespace; empty for cluster-scoped
	SourceFiles []string // which files contributed this entry
}

// ValidationStatus indicates whether a GVK is compatible with the target cluster.
type ValidationStatus string

const (
	StatusOK           ValidationStatus = "OK"
	StatusIncompatible ValidationStatus = "Incompatible"
)

// ValidationResult is one row in the final report.
type ValidationResult struct {
	APIVersion     string           `json:"apiVersion" yaml:"apiVersion"`
	Kind           string           `json:"kind" yaml:"kind"`
	Namespace      string           `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	ResourcePlural string           `json:"resourcePlural,omitempty" yaml:"resourcePlural,omitempty"`
	Status         ValidationStatus `json:"status" yaml:"status"`
	Reason         string           `json:"reason,omitempty" yaml:"reason,omitempty"`
	Suggestion     string           `json:"suggestion,omitempty" yaml:"suggestion,omitempty"`
}

// ValidationReport is the complete output.
type ValidationReport struct {
	Mode               string             `json:"mode" yaml:"mode"`                                                 // "live" or "offline"
	APIResourcesSource string             `json:"apiResourcesSource,omitempty" yaml:"apiResourcesSource,omitempty"` // file path (offline mode)
	ClusterContext     string             `json:"clusterContext,omitempty" yaml:"clusterContext,omitempty"`         // kubeconfig context (live mode)
	Results            []ValidationResult `json:"results" yaml:"results"`
	TotalScanned       int                `json:"totalScanned" yaml:"totalScanned"`
	Compatible         int                `json:"compatible" yaml:"compatible"`
	Incompatible       int                `json:"incompatible" yaml:"incompatible"`
}

// HasIncompatible returns true if any resources are incompatible with the target.
func (r *ValidationReport) HasIncompatible() bool { return r.Incompatible > 0 }

// IncompatibleResults returns only the results with Incompatible status.
func (r *ValidationReport) IncompatibleResults() []ValidationResult {
	var out []ValidationResult
	for _, res := range r.Results {
		if res.Status == StatusIncompatible {
			out = append(out, res)
		}
	}
	return out
}

// ErrValidationFailed is returned when one or more validation checks fail,
// giving CI/CD pipelines a non-zero exit code.
var ErrValidationFailed = fmt.Errorf("validation failed: one or more resources are incompatible with the target cluster")
