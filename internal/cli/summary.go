package cli

import (
	"fmt"
	"io"
	"time"
)

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
