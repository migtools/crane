package e2e

import (
	"flag"
	"log"
	"testing"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// init registers CLI flags used by the e2e test suite.
func init() {
	flag.StringVar(&config.CraneBin, "crane-bin", "crane", "Path to crane binary")
	flag.StringVar(&config.TargetContext, "target-context", "", "Target cluster context for crane apply/validation")
	flag.StringVar(&config.SourceContext, "source-context", "", "Source cluster context for app deploy tests")
	flag.StringVar(&config.K8sDeployBin, "k8sdeploy-bin", "k8sdeploy", "Path to k8sdeploy binary")
	flag.BoolVar(&config.VerboseLogs, "verbose-logs", false, "Enable verbose command/output logs for e2e runners")
	flag.StringVar(&config.SourceNonAdminContext, "source-nonadmin-context", "", "Source cluster non-admin context for RBAC scenarios")
	flag.StringVar(&config.TargetNonAdminContext, "target-nonadmin-context", "", "Target cluster non-admin context for RBAC scenarios")
}

// TestE2E configures Ginkgo and executes the e2e test suite.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	reporterConfig.Verbose = true
	log.SetOutput(GinkgoWriter)
	log.SetFlags(log.LstdFlags)
	RunSpecs(t, "E2E Suite", suiteConfig, reporterConfig)
}
