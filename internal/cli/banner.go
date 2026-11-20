package cli

import (
	"fmt"
	"io"
)

// OutputField is a labeled value written in a banner or Summary.
type OutputField struct {
	Label string
	Value string
}

// PrintBanner writes a command title and its labeled values to w.
func PrintBanner(w io.Writer, title string, fields []OutputField) {
	fmt.Fprintf(w, "\n%s\n", title)
	for _, field := range fields {
		fmt.Fprintf(w, "%s: %s\n", field.Label, field.Value)
	}
	fmt.Fprintln(w)
}

func PrintTransferBanner(w io.Writer, sourceCtx, destCtx, pvcMapping, endpoint, subdomain string) {
	fmt.Fprintf(w, "\ncrane transfer-pvc\n")
	fmt.Fprintf(w, "source context:      %s\n", sourceCtx)
	fmt.Fprintf(w, "destination context: %s\n", destCtx)
	fmt.Fprintf(w, "PVC:                 %s\n", pvcMapping)
	if subdomain != "" {
		fmt.Fprintf(w, "endpoint:            %s  (subdomain: %s)\n", endpoint, subdomain)
	} else {
		fmt.Fprintf(w, "endpoint:            %s\n", endpoint)
	}
	fmt.Fprintln(w)
}
