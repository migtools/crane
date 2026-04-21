package validate

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/sirupsen/logrus"
	sigsyaml "sigs.k8s.io/yaml"
)

// FormatTable writes a human-readable table to w.
func FormatTable(w io.Writer, report *ValidationReport) {
	table := tablewriter.NewWriter(w)
	table.SetHeader([]string{"APIVERSION", "KIND", "NAMESPACE", "RESOURCE", "STATUS", "REASON", "SUGGESTION"})
	table.SetAutoWrapText(false)
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, r := range report.Results {
		table.Append([]string{
			r.APIVersion,
			r.Kind,
			r.Namespace,
			r.ResourcePlural,
			string(r.Status),
			r.Reason,
			r.Suggestion,
		})
	}

	table.Render()
	fmt.Fprintf(w, "\nSummary: %d scanned, %d compatible, %d incompatible\n",
		report.TotalScanned, report.Compatible, report.Incompatible)
}

// FormatJSON writes the report as indented JSON to w.
func FormatJSON(w io.Writer, report *ValidationReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// FormatYAML writes the report as YAML to w.
func FormatYAML(w io.Writer, report *ValidationReport) error {
	data, err := sigsyaml.Marshal(report)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// WriteFailures writes incompatible results as individual YAML files under
// failuresDir, following the same pattern used by the export command's
// failures/ directory. Each file is named by apiVersion-kind-namespace.yaml.
func WriteFailures(failuresDir string, report *ValidationReport, log logrus.FieldLogger) error {
	incompatible := report.IncompatibleResults()
	if len(incompatible) == 0 {
		return nil
	}

	if err := os.RemoveAll(failuresDir); err != nil {
		return fmt.Errorf("clear validate failures directory %q: %w", failuresDir, err)
	}
	if err := os.MkdirAll(failuresDir, 0700); err != nil {
		return fmt.Errorf("create validate failures directory %q: %w", failuresDir, err)
	}

	for _, r := range incompatible {
		filename := failureFileName(r)
		path := filepath.Join(failuresDir, filename)

		data, err := sigsyaml.Marshal(r)
		if err != nil {
			log.Warnf("error marshaling failure for %s/%s: %v", r.APIVersion, r.Kind, err)
			continue
		}

		if err := os.WriteFile(path, data, 0600); err != nil {
			log.Warnf("error writing failure file %s: %v", path, err)
			continue
		}
		log.Debugf("wrote validation failure: %s", path)
	}

	log.Infof("Wrote %d validation failure(s) to %s", len(incompatible), failuresDir)
	return nil
}

// failureFileName builds a stable filename from a ValidationResult.
// Format: Kind_group_version_namespace.yaml (matching export's naming pattern).
func failureFileName(r ValidationResult) string {
	group, version := parseAPIVersion(r.APIVersion)
	ns := r.Namespace
	if ns == "" {
		ns = "clusterscoped"
	}
	return strings.Join([]string{r.Kind, group, version, ns}, "_") + ".yaml"
}

// parseAPIVersion splits "apps/v1" into ("apps","v1") and "v1" into ("","v1").
func parseAPIVersion(apiVersion string) (string, string) {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 1 {
		return "", parts[0]
	}
	return parts[0], parts[1]
}
