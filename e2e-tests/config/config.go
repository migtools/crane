package config

import (
	"fmt"
	"log"
)

var (
	K8sDeployBin          string
	SourceContext         string
	CraneBin              string
	TargetContext         string
	VerboseLogs           bool
	SourceNonAdminContext string
	TargetNonAdminContext string
	InsecureSkipTLSVerify bool
	RunAs                 string
)

// Validates the --run-as flag value and logs the active mode.
// This should be called in BeforeSuite hooks in test suites.
// Returns an error if the --run-as value is invalid.
func ValidateAndLogRunAsFlag() error {
	// Validate --run-as flag value
	if RunAs != "" && RunAs != "admin" {
		return fmt.Errorf("invalid --run-as value: %q. Valid values: \"admin\" or empty string (for non-admin mode)", RunAs)
	}

	if RunAs == "admin" {
		log.Printf("[E2E] Running tests with --run-as=admin (cluster-admin credentials)")
	} else {
		log.Printf("[E2E] Running tests in non-admin mode")
	}
	return nil
}
