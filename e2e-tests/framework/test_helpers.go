package framework

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
)

// mtaIDPattern matches MTA ticket references like [MTA-801] in spec descriptions.
var mtaIDPattern = regexp.MustCompile(`\[MTA-\d+\]`)

// ExtractMTAID extracts the MTA-XXX identifier (without brackets) from a spec's full text.
// Returns an empty string if no MTA ID is found.
func ExtractMTAID(fullText string) string {
	match := mtaIDPattern.FindString(fullText)
	return strings.Trim(match, "[]")
}

// RegisterMTAResultReporter registers a ReportAfterEach hook that prints a
// [CRANE-RESULT] line for every spec whose description contains an MTA ID,
// so CI can parse and correlate results back to Polarion/MTA tickets.
func RegisterMTAResultReporter() {
	ginkgo.ReportAfterEach(func(report types.SpecReport) {
		id := ExtractMTAID(report.FullText())
		if id == "" {
			return
		}
		switch report.State {
		case types.SpecStatePassed:
			fmt.Printf("\n[CRANE-RESULT] PASSED %s\n", id)
		case types.SpecStateFailed, types.SpecStateTimedout, types.SpecStatePanicked:
			fmt.Printf("\n[CRANE-RESULT] FAILED %s\n", id)
		case types.SpecStateSkipped:
			fmt.Printf("\n[CRANE-RESULT] SKIPPED %s\n", id)
		}
	})
}

type ExpectedClusterRoleBinding struct {
	ClusterRoleBindingName string
	ClusterRoleName        string
	SubjectName            string
}

func ValidateClusterRBAC(kubectl KubectlRunner, bindings []ExpectedClusterRoleBinding) error {
	clusterRoles := map[string]bool{}
	for _, b := range bindings {
		clusterRoles[b.ClusterRoleName] = true
	}
	for cr := range clusterRoles {
		if _, err := kubectl.Run("get", "clusterrole", cr); err != nil {
			return fmt.Errorf("ClusterRole %s not found: %w", cr, err)
		}
		log.Printf("ClusterRole %s exists", cr)
	}

	for _, b := range bindings {
		if _, err := kubectl.Run("get", "clusterrolebinding", b.ClusterRoleBindingName); err != nil {
			return fmt.Errorf("ClusterRoleBinding %s not found: %w", b.ClusterRoleBindingName, err)
		}

		roleRef, err := kubectl.Run("get", "clusterrolebinding", b.ClusterRoleBindingName, "-o", "jsonpath={.roleRef.name}")
		if err != nil {
			return fmt.Errorf("failed to get roleRef for CRB %s: %w", b.ClusterRoleBindingName, err)
		}
		if roleRef != b.ClusterRoleName {
			return fmt.Errorf("CRB %s references %s, expected %s", b.ClusterRoleBindingName, roleRef, b.ClusterRoleName)
		}

		subject, err := kubectl.Run("get", "clusterrolebinding", b.ClusterRoleBindingName, "-o", "jsonpath={.subjects[*].name}")
		if err != nil {
			return fmt.Errorf("failed to get subject for CRB %s: %w", b.ClusterRoleBindingName, err)
		}
		if subject != b.SubjectName {
			return fmt.Errorf("CRB %s subject is %s, expected %s", b.ClusterRoleBindingName, subject, b.SubjectName)
		}
		log.Printf("CRB %s -> CR %s (subject: %s) verified", b.ClusterRoleBindingName, b.ClusterRoleName, b.SubjectName)
	}
	return nil
}
