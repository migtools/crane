package cli

import (
	"fmt"
	"io"
	"time"
)

// PrintSummary writes labeled command results and an optional completion marker to w.
func PrintSummary(w io.Writer, fields []OutputField, done bool) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Summary")
	fmt.Fprintln(w, "-------")
	for _, field := range fields {
		fmt.Fprintf(w, "%s: %s\n", field.Label, field.Value)
	}
	if done {
		fmt.Fprintln(w, "Done.")
	}
}

type TransferSummary struct {
	Status   string
	Duration time.Duration
}

func PrintTransferSummary(w io.Writer, s *TransferSummary) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Summary")
	fmt.Fprintln(w, "-------")
	fmt.Fprintf(w, "PVC data copy: %s\n", s.Status)
	fmt.Fprintf(w, "duration:      %s\n", s.Duration.Round(time.Second))
	if s.Status == "succeeded" {
		fmt.Fprintln(w, "Done.")
	}
}
