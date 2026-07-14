package config

import (
	"log"

	"github.com/onsi/ginkgo/v2"
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

// ValidateAndLogRunAsFlag validates the --run-as flag and logs the mode
func ValidateAndLogRunAsFlag() {
	if RunAs != "" && RunAs != "admin" {
		log.Fatalf("Invalid --run-as value: %q. Valid values: \"admin\" or empty string (for non-admin mode)", RunAs)
	}

	if RunAs == "admin" {
		ginkgo.GinkgoWriter.Printf("Running e2e suite in admin mode\n")
	} else {
		ginkgo.GinkgoWriter.Printf("Running e2e suite in non-admin mode\n")
	}
}