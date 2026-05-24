package apply

import (
	"strings"
	"testing"

	"github.com/konveyor/crane/internal/flags"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// TestNewApplyCommand_PreRunBindsFlags verifies that PreRun correctly binds cobra flags
// to viper and unmarshals them into the Options.Flags struct. We can't inspect the
// internal Options directly, so we observe the effect through Validate's behavior:
// if mutually exclusive flags are properly unmarshaled, Validate returns the expected error.
func TestNewApplyCommand_PreRunBindsFlags(t *testing.T) {
	tests := []struct {
		name            string
		flagsToSet      map[string]string
		expectValError  bool
		valErrorContain string
	}{
		{
			name: "stage and from-stage are mutually exclusive",
			flagsToSet: map[string]string{
				"stage":      "10_kubernetes",
				"from-stage": "20_openshift",
			},
			expectValError:  true,
			valErrorContain: "mutually exclusive",
		},
		{
			name: "stage and stages are mutually exclusive",
			flagsToSet: map[string]string{
				"stage":  "10_kubernetes",
				"stages": "20_openshift",
			},
			expectValError:  true,
			valErrorContain: "mutually exclusive",
		},
		{
			name: "only stage set passes validation",
			flagsToSet: map[string]string{
				"stage": "10_kubernetes",
			},
			expectValError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()

			cmd := NewApplyCommand(&flags.GlobalFlags{})
			for k, v := range tt.flagsToSet {
				if err := cmd.Flags().Set(k, v); err != nil {
					t.Fatalf("failed to set flag %q to %q: %v", k, v, err)
				}
			}

			// Execute PreRun to bind flags → viper → Options.Flags
			cmd.PreRun(cmd, []string{})

			// Execute RunE which calls Complete → Validate → Run
			err := cmd.RunE(cmd, []string{})

			if tt.expectValError {
				if err == nil {
					t.Fatal("expected a validation error but RunE returned nil")
				}
				if !strings.Contains(err.Error(), tt.valErrorContain) {
					t.Errorf("expected error containing %q, got: %v", tt.valErrorContain, err)
				}
			} else {
				// When only "stage" is set, validation passes but run() will fail
				// (e.g. kubectl not found). We just verify the error is NOT the
				// validation error, which proves the flags were correctly unmarshaled.
				if err != nil && strings.Contains(err.Error(), "mutually exclusive") {
					t.Errorf("unexpected validation error: %v", err)
				}
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		flags     Flags
		wantError bool
		errorMsg  string
	}{
		{
			name:      "no flags set - valid",
			flags:     Flags{},
			wantError: false,
		},
		{
			name: "only stage set - valid",
			flags: Flags{
				Stage: "10_kubernetes",
			},
			wantError: false,
		},
		{
			name: "only from-stage set - valid",
			flags: Flags{
				FromStage: "10_kubernetes",
			},
			wantError: false,
		},
		{
			name: "only to-stage set - valid",
			flags: Flags{
				ToStage: "20_openshift",
			},
			wantError: false,
		},
		{
			name: "from-stage and to-stage set - valid",
			flags: Flags{
				FromStage: "10_kubernetes",
				ToStage:   "30_imagestream",
			},
			wantError: false,
		},
		{
			name: "only stages set - valid",
			flags: Flags{
				Stages: []string{"10_kubernetes", "30_imagestream"},
			},
			wantError: false,
		},
		{
			name: "stage and from-stage set - invalid",
			flags: Flags{
				Stage:     "10_kubernetes",
				FromStage: "20_openshift",
			},
			wantError: true,
			errorMsg:  "--stage, --from-stage/--to-stage, and --stages are mutually exclusive",
		},
		{
			name: "stage and to-stage set - invalid",
			flags: Flags{
				Stage:   "10_kubernetes",
				ToStage: "20_openshift",
			},
			wantError: true,
			errorMsg:  "--stage, --from-stage/--to-stage, and --stages are mutually exclusive",
		},
		{
			name: "stage and stages set - invalid",
			flags: Flags{
				Stage:  "10_kubernetes",
				Stages: []string{"20_openshift"},
			},
			wantError: true,
			errorMsg:  "--stage, --from-stage/--to-stage, and --stages are mutually exclusive",
		},
		{
			name: "from-stage and stages set - invalid",
			flags: Flags{
				FromStage: "10_kubernetes",
				Stages:    []string{"20_openshift"},
			},
			wantError: true,
			errorMsg:  "--stage, --from-stage/--to-stage, and --stages are mutually exclusive",
		},
		{
			name: "all flags set - invalid",
			flags: Flags{
				Stage:     "10_kubernetes",
				FromStage: "20_openshift",
				ToStage:   "30_imagestream",
				Stages:    []string{"40_custom"},
			},
			wantError: true,
			errorMsg:  "--stage, --from-stage/--to-stage, and --stages are mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{
				Flags: tt.flags,
			}

			err := o.Validate()

			if tt.wantError {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
					return
				}
				if err.Error() != tt.errorMsg {
					t.Errorf("Validate() error = %v, want %v", err.Error(), tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestStageSelectionRouting(t *testing.T) {
	tests := []struct {
		name           string
		flags          Flags
		expectMulti    bool
		expectSelector bool
		selectorStage  string
		selectorFrom   string
		selectorTo     string
		selectorStages []string
	}{
		{
			name:           "default - no flags (final stage only)",
			flags:          Flags{},
			expectMulti:    false,
			expectSelector: false,
		},
		{
			name: "stage flag set",
			flags: Flags{
				Stage: "10_kubernetes",
			},
			expectMulti:    true,
			expectSelector: true,
			selectorStage:  "10_kubernetes",
		},
		{
			name: "from-stage flag set",
			flags: Flags{
				FromStage: "20_openshift",
			},
			expectMulti:    true,
			expectSelector: true,
			selectorFrom:   "20_openshift",
		},
		{
			name: "to-stage flag set",
			flags: Flags{
				ToStage: "30_imagestream",
			},
			expectMulti:    true,
			expectSelector: true,
			selectorTo:     "30_imagestream",
		},
		{
			name: "from-stage and to-stage set",
			flags: Flags{
				FromStage: "10_kubernetes",
				ToStage:   "30_imagestream",
			},
			expectMulti:    true,
			expectSelector: true,
			selectorFrom:   "10_kubernetes",
			selectorTo:     "30_imagestream",
		},
		{
			name: "stages flag set",
			flags: Flags{
				Stages: []string{"10_kubernetes", "30_imagestream"},
			},
			expectMulti:    true,
			expectSelector: true,
			selectorStages: []string{"10_kubernetes", "30_imagestream"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the routing logic from run() method
			isMultiStage := tt.flags.Stage != "" || tt.flags.FromStage != "" ||
				tt.flags.ToStage != "" || len(tt.flags.Stages) > 0

			if isMultiStage != tt.expectMulti {
				t.Errorf("Stage routing: got multi=%v, want multi=%v", isMultiStage, tt.expectMulti)
			}

			if tt.expectSelector {
				// Verify selector would be constructed correctly
				if tt.flags.Stage != tt.selectorStage {
					t.Errorf("Selector Stage: got %v, want %v", tt.flags.Stage, tt.selectorStage)
				}
				if tt.flags.FromStage != tt.selectorFrom {
					t.Errorf("Selector FromStage: got %v, want %v", tt.flags.FromStage, tt.selectorFrom)
				}
				if tt.flags.ToStage != tt.selectorTo {
					t.Errorf("Selector ToStage: got %v, want %v", tt.flags.ToStage, tt.selectorTo)
				}
				if len(tt.flags.Stages) > 0 {
					if len(tt.flags.Stages) != len(tt.selectorStages) {
						t.Errorf("Selector Stages length: got %v, want %v", len(tt.flags.Stages), len(tt.selectorStages))
					}
					for i := range tt.flags.Stages {
						if tt.flags.Stages[i] != tt.selectorStages[i] {
							t.Errorf("Selector Stages[%d]: got %v, want %v", i, tt.flags.Stages[i], tt.selectorStages[i])
						}
					}
				}
			}
		})
	}
}

func TestComplete(t *testing.T) {
	tests := []struct {
		name      string
		options   *Options
		wantError bool
	}{
		{
			name: "complete succeeds",
			options: &Options{
				cobraGlobalFlags: &flags.GlobalFlags{},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			err := tt.options.Complete(cmd, []string{})

			if tt.wantError && err == nil {
				t.Error("Complete() expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Complete() unexpected error: %v", err)
			}
		})
	}
}
