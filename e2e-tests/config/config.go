package config

import "log"

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
func ValidateAndLogRunAsFlag() {
	// Validate --run-as flag value
	if RunAs != "" && RunAs != "admin" {
		log.Fatalf("Invalid --run-as value: %q. Valid values: \"admin\" or empty string (for non-admin mode)", RunAs)
	}

	if RunAs == "admin" {
		log.Printf("[E2E] Running tests with --run-as=admin (cluster-admin credentials)")
	} else {
		log.Printf("[E2E] Running tests in non-admin mode")
	}
}
