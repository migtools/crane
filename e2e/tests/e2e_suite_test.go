package e2e

import (
	"flag"
	"testing"

	"github.com/konveyor/crane/e2e/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func init() {
	flag.StringVar(&config.CraneBin, "crane-bin", "/Users/mmansoor/Desktop/git_repos/crane-testing/crane/crane", "Path to crane binary")
	flag.StringVar(&config.TargetContext, "target-context", "", "Target cluster context for crane apply/validation")
	flag.StringVar(&config.SourceContext, "source-context", "", "Source cluster context for app deploy tests")
	flag.StringVar(&config.K8sDeployBin, "k8sdeploy-bin", "/Users/mmansoor/Desktop/git_repos/k8s-apps-deployer/venv/bin/k8sdeploy", "Path to k8sdeploy binary")
}
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	reporterConfig.Verbose = true
	RunSpecs(t, "E2E Suite", suiteConfig, reporterConfig)
}
