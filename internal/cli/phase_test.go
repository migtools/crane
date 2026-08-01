package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPhaseStartEnd(t *testing.T) {
	var buf bytes.Buffer
	p := NewPhaseTracker(&buf, 3)

	p.Start("Reading source PVC")
	p.End("ok", "")

	output := buf.String()
	if !strings.Contains(output, "[1/3] Reading source PVC ...") {
		t.Errorf("Start output missing, got: %q", output)
	}
	if !strings.Contains(output, "[1/3] Reading source PVC ... ok") {
		t.Errorf("End output missing, got: %q", output)
	}
}

func TestPhaseEndWithDetail(t *testing.T) {
	var buf bytes.Buffer
	p := NewPhaseTracker(&buf, 1)

	p.Start("Copying data")
	p.End("finished", "exit=0")

	output := buf.String()
	if !strings.Contains(output, "finished  exit=0") {
		t.Errorf("Detail not shown, got: %q", output)
	}
}

func TestPhaseFail(t *testing.T) {
	var buf bytes.Buffer
	p := NewPhaseTracker(&buf, 1)

	origErr := errors.New("not found")
	p.Start("Reading source PVC")
	err := p.Fail(origErr, "unable to get source PVC")

	output := buf.String()
	if !strings.Contains(output, "FAILED") {
		t.Errorf("FAILED not shown, got: %q", output)
	}
	if !strings.Contains(output, "error: not found") {
		t.Errorf("Error message not shown, got: %q", output)
	}
	if err == nil {
		t.Error("Fail should return an error")
	}
	if !strings.Contains(err.Error(), "unable to get source PVC") {
		t.Errorf("Error should contain message, got: %v", err)
	}
	if !errors.Is(err, origErr) {
		t.Errorf("Error should wrap original, got: %v", err)
	}
}

func TestSequentialPhases(t *testing.T) {
	var buf bytes.Buffer
	p := NewPhaseTracker(&buf, 3)

	p.Start("Phase A")
	p.End("ok", "")
	p.Start("Phase B")
	p.End("ok", "")
	p.Start("Phase C")
	p.End("ok", "")

	output := buf.String()
	if !strings.Contains(output, "[1/3] Phase A") {
		t.Errorf("Phase 1 missing, got: %q", output)
	}
	if !strings.Contains(output, "[2/3] Phase B") {
		t.Errorf("Phase 2 missing, got: %q", output)
	}
	if !strings.Contains(output, "[3/3] Phase C") {
		t.Errorf("Phase 3 missing, got: %q", output)
	}
}

func TestElapsed(t *testing.T) {
	var buf bytes.Buffer
	p := NewPhaseTracker(&buf, 1)

	elapsed := p.Elapsed()
	if elapsed < 0 {
		t.Errorf("Elapsed should be non-negative, got: %v", elapsed)
	}
}

func TestPrintTransferBannerWithSubdomain(t *testing.T) {
	var buf bytes.Buffer
	PrintTransferBanner(&buf, "src", "tgt", "app/data → app/data", "nginx-ingress", "data.app.example.com")

	output := buf.String()
	for _, want := range []string{"crane transfer-pvc", "source context:      src", "destination context: tgt", "app/data → app/data", "nginx-ingress", "data.app.example.com"} {
		if !strings.Contains(output, want) {
			t.Errorf("Banner missing %q, got: %q", want, output)
		}
	}
}

func TestPrintTransferBannerWithoutSubdomain(t *testing.T) {
	var buf bytes.Buffer
	PrintTransferBanner(&buf, "src", "tgt", "app/data → app/data", "route", "")

	output := buf.String()
	if strings.Contains(output, "subdomain") {
		t.Errorf("Banner should not show subdomain when empty, got: %q", output)
	}
	if !strings.Contains(output, "route") {
		t.Errorf("Banner missing endpoint, got: %q", output)
	}
}

func TestPrintTransferSummarySucceeded(t *testing.T) {
	var buf bytes.Buffer
	PrintTransferSummary(&buf, &TransferSummary{Status: "succeeded", Duration: 45 * time.Second})

	output := buf.String()
	if !strings.Contains(output, "Summary") {
		t.Errorf("Summary header missing, got: %q", output)
	}
	if !strings.Contains(output, "succeeded") {
		t.Errorf("Status missing, got: %q", output)
	}
	if !strings.Contains(output, "45s") {
		t.Errorf("Duration missing, got: %q", output)
	}
	if !strings.Contains(output, "Done.") {
		t.Errorf("Done missing for succeeded, got: %q", output)
	}
}

func TestPrintTransferSummaryFailed(t *testing.T) {
	var buf bytes.Buffer
	PrintTransferSummary(&buf, &TransferSummary{Status: "failed", Duration: 4 * time.Second})

	output := buf.String()
	if !strings.Contains(output, "failed") {
		t.Errorf("Status missing, got: %q", output)
	}
	if !strings.Contains(output, "4s") {
		t.Errorf("Duration missing, got: %q", output)
	}
	if strings.Contains(output, "Done.") {
		t.Errorf("Done should not appear for failed, got: %q", output)
	}
}
