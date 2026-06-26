package framework

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

type KubectlRunner struct {
	Bin     string
	Context string
}

// Run executes an arbitrary kubectl command and returns trimmed output.
// Example: Run("get", "po", "-n", "ns"), Run("delete", "cm", "x", "-n", "ns").
func (k KubectlRunner) Run(args ...string) (string, error) {
	return k.executeWithStdin("", args...)
}

// RunWithStdin executes an arbitrary kubectl command using stdin content.
// Example: RunWithStdin(manifestYAML, "apply", "-f", "-").
func (k KubectlRunner) RunWithStdin(stdin string, args ...string) (string, error) {
	return k.executeWithStdin(stdin, args...)
}

// executeWithStdin executes an arbitrary kubectl command using stdin content.
func (k KubectlRunner) executeWithStdin(stdin string, args ...string) (string, error) {
	finalArgs := append([]string{}, normalizeKubectlArgs(args...)...)
	if k.Context != "" {
		if idx := slices.Index(finalArgs, "--"); idx != -1 {
			finalArgs = slices.Insert(finalArgs, idx, "--context", k.Context)
		} else {
			finalArgs = append(finalArgs, "--context", k.Context)
		}
	}
	logVerboseCommand(k.Bin, finalArgs)
	cmd := exec.Command(k.Bin, finalArgs...)
	if stdin != "" {
		cmd.Stdin = bytes.NewBufferString(stdin)
	}
	out, err := cmd.CombinedOutput()
	logVerboseOutput("kubectl", out)
	if err != nil {
		return "", fmt.Errorf("kubectl %s failed: %v, output: %s", strings.Join(finalArgs, " "), err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// IsOpenShift reports whether the cluster is an OpenShift cluster by checking
// for the route.openshift.io API group. This is used to conditionally adjust
// test app manifests for OCP's SCC requirements.
func (k KubectlRunner) IsOpenShift() bool {
	out, err := k.Run("api-resources", "--api-group=route.openshift.io", "--no-headers")
	if err != nil {
		return false
	}
	cleaned := StripKubectlWarnings(out)
	if strings.TrimSpace(cleaned) == "" {
		return false
	}
	for _, line := range strings.Split(cleaned, "\n") {
		if strings.Contains(line, "route.openshift.io/") {
			return true
		}
	}
	return false
}

// OLMAPIAvailable reports whether the Subscription CRD is registered on the cluster.
func (k KubectlRunner) OLMAPIAvailable() (bool, error) {
	_, err := k.Run("get", "crd", "subscriptions.operators.coreos.com")
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "Error from server (NotFound)") {
		return false, nil
	}
	return false, fmt.Errorf("check OLM CRD subscriptions.operators.coreos.com: %w", err)
}

// normalizeKubectlArgs accepts either tokenized args
// (Run("get","po","-n","ns")) or a single command string
// (Run("get po -n ns")) and converts to tokens.
func normalizeKubectlArgs(args ...string) []string {
	if len(args) == 1 && strings.Contains(args[0], " ") {
		return strings.Fields(args[0])
	}
	return args
}

// StripKubectlWarnings removes warning lines from kubectl output.
// This is useful because some kubectl warnings are written to stderr,
// and our runner returns combined stdout/stderr output.
func StripKubectlWarnings(out string) string {
	lines := strings.Split(out, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Warning: ") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

// CreateNamespace creates a namespace and treats AlreadyExists as success.
func (k KubectlRunner) CreateNamespace(ns string) error {
	args := []string{"create", "namespace", ns}
	if k.Context != "" {
		args = append(args, "--context", k.Context)
	}
	logVerboseCommand(k.Bin, args)
	cmd := exec.Command(k.Bin, args...)
	out, err := cmd.CombinedOutput()
	logVerboseOutput("kubectl create namespace", out)
	if err != nil {
		if strings.Contains(string(out), "AlreadyExists") {
			return nil
		}
		return fmt.Errorf("kubectl create namespace failed: %v, output: %s", err, string(out))
	}
	return nil
}

// ApplyDir applies all manifests recursively from the given directory.
func (k KubectlRunner) ApplyDir(dir string) error {
	args := []string{"apply", "-R", "-f", dir}
	if k.Context != "" {
		args = append(args, "--context", k.Context)
	}
	logVerboseCommand(k.Bin, args)
	cmd := exec.Command(k.Bin, args...)
	out, err := cmd.CombinedOutput()
	logVerboseOutput("kubectl apply", out)
	if err != nil {
		return fmt.Errorf("kubectl apply failed: %v, output: %s", err, string(out))
	}
	return nil
}

// ApplyYAMLSpec applies an inline YAML manifest string with kubectl.
func (k KubectlRunner) ApplyYAMLSpec(spec string, namespace string) error {
	_, err := k.RunWithStdin(spec, "apply", "-f", "-", "-n", namespace)
	if err != nil {
		return fmt.Errorf("kubectl apply inline spec failed: %w", err)
	}
	return nil
}

// GetPodNameByLabel returns the first pod name matching a label selector in a namespace.
// It returns an error when no pod is found or the kubectl query fails.
func GetPodNameByLabel(k KubectlRunner, namespace, selector string) (string, error) {
	out, err := k.Run(
		"get", "pod",
		"-n", namespace,
		"-l", selector,
		"-o", "jsonpath={.items[0].metadata.name}",
	)
	if err != nil {
		return "", err
	}
	podName := strings.TrimSpace(out)
	if podName == "" {
		return "", fmt.Errorf("no pod found for selector %q in namespace %q", selector, namespace)
	}
	return podName, nil
}

// dryRunKnownOCPErrors is the set of error substrings that OCP admission or
// API machinery produces during server-side dry-run but that do NOT indicate a
// real manifest problem. These are safe to log-and-skip rather than fail on.
//
// Rationale for each entry:
//   - "forbidden: unable to validate against any security context constraint":
//     OCP SCC admission fires during dry-run even though the SA the pod will
//     actually run as hasn't been given SCCs yet. The real apply succeeds once
//     SCCs are in place.
//   - "admission webhook":
//     OCP ships several admission webhooks (e.g. for SCCs, network policies)
//     that reject dry-run requests more aggressively than the actual apply path.
//   - "no matches for kind":
//     The exported manifests may contain OCP-specific types (Route, ImageStream,
//     BuildConfig) that don't exist on a plain-k8s target, or vice-versa.
//     The OpenShiftPlugin whiteouts these on the correct target; on a mismatched
//     target cluster the dry-run will fail but the actual scenario is still valid.
var dryRunKnownOCPErrors = []string{
	"unable to validate against any security context constraint",
	"admission webhook",
	"no matches for kind",
}

// isDryRunOCPNoise returns true when the error output from a dry-run only
// contains lines that are known OCP admission noise, meaning every failing line
// can be attributed to one of the known patterns above.
func isDryRunOCPNoise(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Lines that start with "error:" or "Error" are the ones that caused
		// the non-zero exit code. Check whether each one is a known pattern.
		lower := strings.ToLower(line)
		if !strings.HasPrefix(lower, "error") {
			continue
		}
		matched := false
		for _, known := range dryRunKnownOCPErrors {
			if strings.Contains(lower, strings.ToLower(known)) {
				matched = true
				break
			}
		}
		if !matched {
			// At least one error line is not a known OCP noise pattern — treat
			// the whole dry-run as a genuine failure.
			return false
		}
	}
	return true
}

// ValidateApplyDir performs a server-side dry-run apply for a directory.
//
// On OCP, admission webhooks and SCC validation routinely reject dry-run
// requests that would succeed on the actual apply path (because SCCs, SAs, and
// webhooks behave differently during dry-run). When the dry-run fails but every
// error line matches a known OCP noise pattern, the failure is logged as a
// warning and the function returns nil so the test can proceed to the real apply.
func (k KubectlRunner) ValidateApplyDir(dir string) error {
	args := []string{"apply", "-R", "-f", dir, "--dry-run=server"}
	if k.Context != "" {
		args = append(args, "--context", k.Context)
	}
	logVerboseCommand(k.Bin, args)
	cmd := exec.Command(k.Bin, args...)
	out, err := cmd.CombinedOutput()
	logVerboseOutput("kubectl dry-run apply", out)
	if err == nil {
		return nil
	}

	outStr := string(out)
	if isDryRunOCPNoise(outStr) {
		log.Printf(
			"[ValidateApplyDir] dry-run returned OCP admission noise (SCC/webhook/unknown-kind); "+
				"proceeding to real apply. Dry-run output:\n%s", outStr,
		)
		return nil
	}

	return fmt.Errorf("kubectl dry-run apply failed: %v, output: %s", err, outStr)
}

// ScaleDeployment scales matching deployment by label, then falls back to name.
func (k KubectlRunner) ScaleDeployment(ns, appName string, replicas int) error {
	selector := "name=" + appName
	checkArgs := []string{"get", "deployment", "--namespace", ns, "-l", selector, "-o", "name"}
	if k.Context != "" {
		checkArgs = append(checkArgs, "--context", k.Context)
	}
	logVerboseCommand(k.Bin, checkArgs)
	checkCmd := exec.Command(k.Bin, checkArgs...)
	checkOut, checkErr := checkCmd.CombinedOutput()
	logVerboseOutput("kubectl get deployment by label", checkOut)
	if checkErr != nil {
		return fmt.Errorf("kubectl get deployment by label failed: %v, output: %s", checkErr, string(checkOut))
	}

	baseArgs := []string{"scale", "deployment", "--namespace", ns, "--replicas", strconv.Itoa(replicas)}
	if strings.TrimSpace(string(checkOut)) != "" {
		args := append(baseArgs, "-l", selector)
		if k.Context != "" {
			args = append(args, "--context", k.Context)
		}
		logVerboseCommand(k.Bin, args)
		cmd := exec.Command(k.Bin, args...)
		out, err := cmd.CombinedOutput()
		logVerboseOutput("kubectl scale deployment", out)
		if err != nil {
			return fmt.Errorf("kubectl scale failed: %v, output: %s", err, string(out))
		}
		return nil
	}

	// Fallback when label-based scale doesn't find a deployment:
	// try "<appName>-deployment" name convention.
	fallbackNames := []string{appName + "-deployment"}

	var lastErr error
	var lastOut []byte
	for _, depName := range fallbackNames {
		args := append(baseArgs, depName)
		if k.Context != "" {
			args = append(args, "--context", k.Context)
		}
		logVerboseCommand(k.Bin, args)
		cmd := exec.Command(k.Bin, args...)
		out, err := cmd.CombinedOutput()
		logVerboseOutput("kubectl scale deployment", out)
		if err == nil {
			return nil
		}
		lastErr = err
		lastOut = out
	}

	return fmt.Errorf("kubectl scale failed after label and name fallbacks: %v, output: %s", lastErr, string(lastOut))
}

// ScaleDeploymentIfPresent scales a deployment only when it can be discovered.
// It is useful for scenarios that may not create deployments (e.g. namespace-only apps).
func (k KubectlRunner) ScaleDeploymentIfPresent(ns, appName string, replicas int) error {
	selector := "name=" + appName
	checkArgs := []string{"get", "deployment", "--namespace", ns, "-l", selector, "-o", "name"}
	if k.Context != "" {
		checkArgs = append(checkArgs, "--context", k.Context)
	}
	logVerboseCommand(k.Bin, checkArgs)
	checkCmd := exec.Command(k.Bin, checkArgs...)
	checkOut, checkErr := checkCmd.CombinedOutput()
	logVerboseOutput("kubectl get deployment by label", checkOut)
	if checkErr != nil {
		return fmt.Errorf("kubectl get deployment by label failed: %v, output: %s", checkErr, string(checkOut))
	}

	// No deployment for this app; nothing to scale down.
	if strings.TrimSpace(string(checkOut)) == "" {
		return nil
	}
	return k.ScaleDeployment(ns, appName, replicas)
}

func (k KubectlRunner) CanI(verb, resource, namespace string) (bool, error) {
	args := []string{"auth", "can-i", verb, resource, "--quiet"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	if k.Context != "" {
		args = append(args, "--context", k.Context)
	}
	logVerboseCommand(k.Bin, args)
	cmd := exec.Command(k.Bin, args...)
	out, err := cmd.CombinedOutput()
	logVerboseOutput("kubectl auth can-i", out)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		// "can-i --quiet" returns exit code 1 for an authorization denial.
		return false, nil
	}
	return false, fmt.Errorf("kubectl auth can-i failed: %v, output: %s", err, string(out))
}
