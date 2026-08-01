package cli

import (
	"fmt"
	"io"
)

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
