package listplugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/konveyor/crane/internal/flags"
)

func TestNewListPluginsCommand_command_structure(t *testing.T) {
	tests := []struct {
		name             string
		globalFlags      *flags.GlobalFlags
		wantUse          string
		wantShortContain string
	}{
		{
			name:             "command has correct Use and Short",
			globalFlags:      &flags.GlobalFlags{},
			wantUse:          "list-plugins",
			wantShortContain: "list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewListPluginsCommand(tt.globalFlags)

			if cmd.Use != tt.wantUse {
				t.Errorf("cmd.Use = %q, want %q", cmd.Use, tt.wantUse)
			}

			if !strings.Contains(strings.ToLower(cmd.Short), tt.wantShortContain) {
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

func TestListPluginsRun(t *testing.T) {
	tests := []struct {
		name        string
		setupDir    func(t *testing.T) string
		wantErr     bool
		errContains string
	}{
		{
			name: "non-existent plugin directory returns no error",
			setupDir: func(t *testing.T) string {
				// Return a path that doesn't exist
				// Note: GetFilteredPlugins handles non-existent dirs gracefully
				return filepath.Join(os.TempDir(), "crane-test-nonexistent-"+t.Name())
			},
			wantErr: false,
		},
		{
			name: "empty plugin directory returns no error",
			setupDir: func(t *testing.T) string {
				dir := t.TempDir()
				return dir
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pluginDir := tt.setupDir(t)

			o := &Options{
				globalFlags: &flags.GlobalFlags{},
				Flags: Flags{
					PluginDir:   pluginDir,
					SkipPlugins: []string{},
				},
			}

			err := o.run()

			if tt.wantErr {
				if err == nil {
					t.Errorf("run() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("run() error = %v, want error containing %q", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("run() unexpected error = %v", err)
				}
			}
		})
	}
}
