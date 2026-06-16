package framework

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
)

// mtaIDPattern matches MTA ticket references like [MTA-801] in spec descriptions.
var mtaIDPattern = regexp.MustCompile(`\[MTA-\d+\]`)

// ExtractMTAID extracts the MTA-XXX identifier (without brackets) from a spec's full text.
// Returns an empty string if no MTA ID is found.
func ExtractMTAID(fullText string) string {
	match := mtaIDPattern.FindString(fullText)
	return strings.Trim(match, "[]")
}

// RegisterMTAResultReporter registers a ReportAfterEach hook that prints a
// [CRANE-RESULT] line for every spec whose description contains an MTA ID,
// so CI can parse and correlate results back to Polarion/MTA tickets.
func RegisterMTAResultReporter() {
	ginkgo.ReportAfterEach(func(report types.SpecReport) {
		id := ExtractMTAID(report.FullText())
		if id == "" {
			return
		}
		switch report.State {
		case types.SpecStatePassed:
			fmt.Printf("\n[CRANE-RESULT] PASSED %s\n", id)
		case types.SpecStateFailed, types.SpecStateTimedout, types.SpecStatePanicked:
			fmt.Printf("\n[CRANE-RESULT] FAILED %s\n", id)
		case types.SpecStateSkipped:
			fmt.Printf("\n[CRANE-RESULT] SKIPPED %s\n", id)
		}
	})
}