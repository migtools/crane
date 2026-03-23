package framework

import (
	"log"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
)

// logVerboseCommand prints the executable and arguments when verbose logs are enabled.
func logVerboseCommand(bin string, args []string) {
	if !config.VerboseLogs {
		return
	}
	log.Printf("[verbose] exec: %s %s", bin, strings.Join(args, " "))
}

// logVerboseOutput prints command output when verbose logs are enabled.
func logVerboseOutput(label string, out []byte) {
	if !config.VerboseLogs || len(out) == 0 {
		return
	}
	log.Printf("[verbose] %s output:\n%s", label, string(out))
}
