package validate

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestFormatTable(t *testing.T) {
	report := &ValidationReport{
		Results: []ValidationResult{
			{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "prod", ResourcePlural: "deployments", Status: StatusOK},
			{APIVersion: "route.openshift.io/v1", Kind: "Route", Namespace: "prod", Status: StatusIncompatible, Reason: "API version route.openshift.io/v1 not available on target cluster"},
		},
		TotalScanned: 2,
		Compatible:   1,
		Incompatible: 1,
	}

	var buf bytes.Buffer
	FormatTable(&buf, report)
	output := buf.String()

	expectedColumns := []string{"APIVERSION", "KIND", "NAMESPACE", "RESOURCE", "STATUS", "REASON", "SUGGESTION"}
	for _, col := range expectedColumns {
		if !strings.Contains(output, col) {
			t.Errorf("table output missing column header %q", col)
		}
	}

	if !strings.Contains(output, "apps/v1") {
		t.Error("table output missing apps/v1")
	}
	if !strings.Contains(output, "route.openshift.io/v1") {
		t.Error("table output missing route.openshift.io/v1")
	}
	if !strings.Contains(output, "OK") {
		t.Error("table output missing OK status")
	}
	if !strings.Contains(output, "Incompatible") {
		t.Error("table output missing Incompatible status")
	}
	if !strings.Contains(output, "2 scanned, 1 compatible, 1 incompatible") {
		t.Errorf("table output missing summary line, got:\n%s", output)
	}
}

func TestFormatTable_EmptyReport(t *testing.T) {
	report := &ValidationReport{}
	var buf bytes.Buffer
	FormatTable(&buf, report)
	output := buf.String()

	if !strings.Contains(output, "0 scanned, 0 compatible, 0 incompatible") {
		t.Errorf("empty report summary incorrect, got:\n%s", output)
	}
}

func TestFormatJSON(t *testing.T) {
	report := &ValidationReport{
		Results: []ValidationResult{
			{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", ResourcePlural: "configmaps", Status: StatusOK},
			{APIVersion: "route.openshift.io/v1", Kind: "Route", Namespace: "prod", Status: StatusIncompatible, Reason: "API version route.openshift.io/v1 not available on target cluster"},
		},
		TotalScanned: 2,
		Compatible:   1,
		Incompatible: 1,
	}

	var buf bytes.Buffer
	if err := FormatJSON(&buf, report); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded ValidationReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON output is not valid: %v", err)
	}
	if decoded.TotalScanned != 2 {
		t.Fatalf("TotalScanned = %d, want 2", decoded.TotalScanned)
	}
	if decoded.Compatible != 1 {
		t.Fatalf("Compatible = %d, want 1", decoded.Compatible)
	}
	if decoded.Incompatible != 1 {
		t.Fatalf("Incompatible = %d, want 1", decoded.Incompatible)
	}
	if len(decoded.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(decoded.Results))
	}
	if decoded.Results[0].Status != StatusOK {
		t.Fatalf("result[0].Status = %s, want OK", decoded.Results[0].Status)
	}
	if decoded.Results[1].Status != StatusIncompatible {
		t.Fatalf("result[1].Status = %s, want Incompatible", decoded.Results[1].Status)
	}
}

func TestFormatJSON_RoundTrip(t *testing.T) {
	original := &ValidationReport{
		Results: []ValidationResult{
			{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "default", ResourcePlural: "deployments", Status: StatusOK},
		},
		TotalScanned: 1,
		Compatible:   1,
		Incompatible: 0,
	}

	var buf bytes.Buffer
	if err := FormatJSON(&buf, original); err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}

	var decoded ValidationReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.TotalScanned != original.TotalScanned ||
		decoded.Compatible != original.Compatible ||
		decoded.Incompatible != original.Incompatible ||
		len(decoded.Results) != len(original.Results) {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", decoded, *original)
	}
}
