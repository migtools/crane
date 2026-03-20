package framework

import (
	"log"
	"strings"

	"github.com/konveyor/crane/e2e/config"
)

func logVerboseCommand(bin string, args []string) {
	if !config.VerboseLogs {
		return
	}
	log.Printf("[verbose] exec: %s %s", bin, strings.Join(args, " "))
}

func logVerboseOutput(label string, out []byte) {
	if !config.VerboseLogs || len(out) == 0 {
		return
	}
	log.Printf("[verbose] %s output:\n%s", label, string(out))
}
