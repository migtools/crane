package indirect_migration

import (
	"flag"
	"log"
	"testing"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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
	flag.StringVar(&config.CloudStorage, "cloud-storage", "", "S3-compatible cloud storage path for indirect transfer (e.g. remote:my-bucket)")
	flag.StringVar(&config.RcloneConfigFile, "rclone-config-file", "", "Path to local rclone.conf file for indirect transfer")
	flag.StringVar(&config.RcloneConfigSecret, "rclone-config-secret", "", "K8s Secret name containing rclone.conf for indirect transfer")
}

var _ = BeforeSuite(func() {
	Expect(config.ValidateAndLogRunAsFlag()).To(Succeed())
})

func TestIndirectMigration(t *testing.T) {
	RegisterFailHandler(Fail)
	RegisterMTAResultReporter()
	suiteConfig, reporterConfig := GinkgoConfiguration()
	reporterConfig.Verbose = true
	log.SetOutput(GinkgoWriter)
	log.SetFlags(log.LstdFlags)
	RunSpecs(t, "Indirect Migration E2E Suite", suiteConfig, reporterConfig)
}
