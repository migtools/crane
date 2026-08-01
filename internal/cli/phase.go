package cli

import (
	"fmt"
	"io"
	"time"
)

type PhaseTracker struct {
	w        io.Writer
	total    int
	current  int
	name     string
	runStart time.Time
}

func NewPhaseTracker(w io.Writer, totalPhases int) *PhaseTracker {
	return &PhaseTracker{
		w:        w,
		total:    totalPhases,
		runStart: time.Now(),
	}
}

func (p *PhaseTracker) Start(name string) {
	p.current++
	p.name = name
	fmt.Fprintf(p.w, "[%d/%d] %s ...\n", p.current, p.total, name)
}

func (p *PhaseTracker) End(status string, detail string) {
	line := fmt.Sprintf("[%d/%d] %s ... %s", p.current, p.total, p.name, status)
	if detail != "" {
		line += "  " + detail
	}
	fmt.Fprintf(p.w, "%s\n", line)
}

func (p *PhaseTracker) Fail(err error, msg string) error {
	p.End("FAILED", "")
	fmt.Fprintf(p.w, "  error: %v\n", err)
	return fmt.Errorf("%s: %w", msg, err)
}

func (p *PhaseTracker) Elapsed() time.Duration {
	return time.Since(p.runStart)
}
