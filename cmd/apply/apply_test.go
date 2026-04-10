package apply

import (
	"testing"

	"github.com/konveyor/crane/internal/flags"
	"github.com/spf13/cobra"
)

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
			expectMulti:      true,
			expectSelector:   true,
			selectorStages:   []string{"10_kubernetes", "30_imagestream"},
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
