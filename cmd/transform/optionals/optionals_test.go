package optionals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/konveyor/crane/internal/flags"
)

func TestNewOptionalsCommand_command_structure(t *testing.T) {
	tests := []struct {
		name             string
		globalFlags      *flags.GlobalFlags
		wantUse          string
		wantShortContain string
	}{
		{
			name:             "command has correct Use and Short",
			globalFlags:      &flags.GlobalFlags{},
			wantUse:          "optionals",
			wantShortContain: "optional",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewOptionalsCommand(tt.globalFlags)

			if cmd.Use != tt.wantUse {
				t.Errorf("cmd.Use = %q, want %q", cmd.Use, tt.wantUse)
			}

			if !strings.Contains(cmd.Short, tt.wantShortContain) {
				t.Errorf("cmd.Short = %q, want it to contain %q", cmd.Short, tt.wantShortContain)
			}

			if cmd.RunE == nil {
				t.Error("cmd.RunE is nil, want it to be set")
			}

			if cmd.PreRun == nil {
				t.Error("cmd.PreRun is nil, want it to be set")
			}
		})
	}
}

func TestOptionalsRun_empty_plugin_dir(t *testing.T) {
	// Create an empty temp directory for plugins
	emptyDir := t.TempDir()

	o := &Options{
		globalFlags: &flags.GlobalFlags{},
		Flags: Flags{
			PluginDir:   emptyDir,
			SkipPlugins: nil,
		},
	}

	err := o.run()
	if err != nil {
		t.Errorf("run() with empty plugin directory returned error: %v, want nil", err)
	}
}

func TestOptionalsRun_nonexistent_dir(t *testing.T) {
	// Create a temp directory and then create a path that doesn't exist within it
	tmpDir := t.TempDir()
	nonexistentDir := filepath.Join(tmpDir, "nonexistent_plugin_dir")

	o := &Options{
		globalFlags: &flags.GlobalFlags{},
		Flags: Flags{
			PluginDir:   nonexistentDir,
			SkipPlugins: nil,
		},
	}

	// According to the implementation, GetPlugins handles os.IsNotExist gracefully
	// and returns the default pluginList (with kubernetes plugin) without error.
	// So this test verifies that non-existent directories don't cause errors.
	err := o.run()
	if err != nil {
		t.Errorf("run() with non-existent plugin directory returned error: %v, want nil", err)
	}
}

func TestOptionalsRun_unreadable_dir(t *testing.T) {
	// Skip this test on systems where we can't change permissions (e.g., running as root)
	if os.Getuid() == 0 {
		t.Skip("Skipping test when running as root")
	}

	// Create a directory that exists but cannot be read due to permissions
	tmpDir := t.TempDir()
	unreadableDir := filepath.Join(tmpDir, "unreadable")
	if err := os.Mkdir(unreadableDir, 0000); err != nil {
		t.Fatalf("Failed to create unreadable directory: %v", err)
	}
	// Ensure we restore permissions so cleanup works
	t.Cleanup(func() {
		os.Chmod(unreadableDir, 0755)
	})

	o := &Options{
		globalFlags: &flags.GlobalFlags{},
		Flags: Flags{
			PluginDir:   unreadableDir,
			SkipPlugins: nil,
		},
	}

	err := o.run()
	if err == nil {
		t.Error("run() with unreadable plugin directory returned nil, want error")
	}
}
