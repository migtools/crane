package e2e

import (
	"flag"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var k8sdeployBin string
var sourceContext string
var craneBin string
var targetContext string

func init() {
	flag.StringVar(&craneBin, "crane-bin", "/Users/mmansoor/Desktop/git_repos/crane-testing/crane/crane", "Path to crane binary")
	flag.StringVar(&targetContext, "target-context", "", "Target cluster context for crane apply/validation")
	flag.StringVar(&sourceContext, "source-context", "", "Source cluster context for app deploy tests")
	flag.StringVar(&k8sdeployBin, "k8sdeploy-bin", "/Users/mmansoor/Desktop/git_repos/k8s-apps-deployer/venv/bin/k8sdeploy", "Path to k8sdeploy binary")
}
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	reporterConfig.Verbose = true
	RunSpecs(t, "E2E Suite", suiteConfig, reporterConfig)
}
