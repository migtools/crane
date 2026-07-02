package e2e

import (
	"flag"
	"log"
	"testing"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
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
	flag.BoolVar(&config.InsecureSkipTLSVerify, "insecure-skip-tls-verify", false, "Skip TLS certificate verification for k8sdeploy connections (use for OCP clusters with self-signed certs)")
	flag.StringVar(&config.RunAs, "run-as", "", "Override user context: set to 'admin' to run all tests with cluster-admin credentials")
}

// TestE2E configures Ginkgo and executes the e2e test suite.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RegisterMTAResultReporter()
	suiteConfig, reporterConfig := GinkgoConfiguration()
	reporterConfig.Verbose = true
	log.SetOutput(GinkgoWriter)
	log.SetFlags(log.LstdFlags)
	RunSpecs(t, "E2E Suite", suiteConfig, reporterConfig)
}