package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
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

	p.Start("Reading source PVC")
	err := p.Fail(errors.New("not found"), "unable to get source PVC")

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
	if !errors.Is(err, errors.New("not found")) {
		// unwrap check
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("Error should wrap original, got: %v", err)
		}
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
