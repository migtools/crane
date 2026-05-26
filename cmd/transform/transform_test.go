package transform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/konveyor/crane/internal/flags"
	"github.com/sirupsen/logrus"
)

// If existing discovered stage dirs exactly match desired config stages,
// reconciliation should succeed.
func TestReconcileConfigStages_NoExtras(t *testing.T) {
	transformDir := t.TempDir()

	// Existing stage dirs
	for _, dir := range []string{"10_KubernetesPlugin", "20_CustomStage"} {
		if err := os.MkdirAll(filepath.Join(transformDir, dir), 0o700); err != nil {
			t.Fatalf("failed to create stage dir %q: %v", dir, err)
		}
	}

	o := &Options{
		Flags: Flags{
			Force: false,
		},
	}

	err := o.reconcileConfigStages(
		transformDir,
		[]string{"10_KubernetesPlugin", "20_CustomStage"},
		logrus.New(),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// If discovered stage dirs include entries not in desired config stages,
// reconciliation should fail in safe mode (without --force).
func TestReconcileConfigStages_ExtraStagesWithoutForce(t *testing.T) {
	transformDir := t.TempDir()

	// Existing dirs include one extra stage not in desired set.
	for _, dir := range []string{"10_KubernetesPlugin", "50_Stage2"} {
		if err := os.MkdirAll(filepath.Join(transformDir, dir), 0o700); err != nil {
			t.Fatalf("failed to create stage dir %q: %v", dir, err)
		}
	}

	o := &Options{
		Flags: Flags{
			Force: false,
		},
	}

	err := o.reconcileConfigStages(
		transformDir,
		[]string{"10_KubernetesPlugin"},
		logrus.New(),
	)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "extra stage directories: 50_Stage2") {
		t.Fatalf("expected error to mention extra stage dir, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "--force") {
		t.Fatalf("expected error to mention --force guidance, got: %s", errMsg)
	}
}

// With --force enabled, extra discovered stage directories should be deleted.
func TestReconcileConfigStages_ExtraStagesWithForceDeletes(t *testing.T) {
	transformDir := t.TempDir()

	// Existing dirs include one desired stage and one extra stage.
	for _, dir := range []string{"10_KubernetesPlugin", "50_Stage2"} {
		if err := os.MkdirAll(filepath.Join(transformDir, dir), 0o700); err != nil {
			t.Fatalf("failed to create stage dir %q: %v", dir, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(transformDir, ".work", "50_Stage2"), 0o700); err != nil {
		t.Fatalf("failed to create stage work dir %q: %v", "50_Stage2", err)
	}
	o := &Options{
		Flags: Flags{
			Force: true,
		},
	}

	err := o.reconcileConfigStages(
		transformDir,
		[]string{"10_KubernetesPlugin"},
		logrus.New(),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Desired stage should still exist.
	if _, err := os.Stat(filepath.Join(transformDir, "10_KubernetesPlugin")); err != nil {
		t.Fatalf("expected desired stage dir to exist, got err: %v", err)
	}

	// Extra stage should be deleted.
	_, err = os.Stat(filepath.Join(transformDir, "50_Stage2"))
	if !os.IsNotExist(err) {
		t.Fatalf("expected extra stage dir to be deleted, got err: %v", err)
	}
	// Extra stage work dir should be deleted.
	_, err = os.Stat(filepath.Join(transformDir, ".work", "50_Stage2"))
	if !os.IsNotExist(err) {
		t.Fatalf("expected extra stage work dir to be deleted, got err: %v", err)
	}
}

// --stage and --config-file are mutually exclusive and should fail fast.
func TestRun_ConfigFileAndStageConflict(t *testing.T) {
	o := &Options{
		globalFlags: &flags.GlobalFlags{},
		Flags: Flags{
			ConfigFile: "sample-transform-instructor-file.yaml",
			Stage:      "10_KubernetesPlugin",
		},
	}

	err := o.run()
	if err == nil {
		t.Fatalf("expected conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "use either --config-file or --stage, not both") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
