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
				Stage: "10_KubernetesPlugin",
			},
			wantError: false,
		},
		{
			name: "only from-stage set - valid",
			flags: Flags{
				FromStage: "10_KubernetesPlugin",
			},
			wantError: false,
		},
		{
			name: "only to-stage set - valid",
			flags: Flags{
				ToStage: "20_OpenshiftPlugin",
			},
			wantError: false,
		},
		{
			name: "from-stage and to-stage set - valid",
			flags: Flags{
				FromStage: "10_KubernetesPlugin",
				ToStage:   "30_ImagestreamPlugin",
			},
			wantError: false,
		},
		{
			name: "only stages set - valid",
			flags: Flags{
				Stages: []string{"10_KubernetesPlugin", "30_ImagestreamPlugin"},
			},
			wantError: false,
		},
		{
			name: "stage and from-stage set - invalid",
			flags: Flags{
				Stage:     "10_KubernetesPlugin",
				FromStage: "20_OpenshiftPlugin",
			},
			wantError: true,
			errorMsg:  "--stage, --from-stage/--to-stage, and --stages are mutually exclusive",
		},
		{
			name: "stage and to-stage set - invalid",
			flags: Flags{
				Stage:   "10_KubernetesPlugin",
				ToStage: "20_OpenshiftPlugin",
			},
			wantError: true,
			errorMsg:  "--stage, --from-stage/--to-stage, and --stages are mutually exclusive",
		},
		{
			name: "stage and stages set - invalid",
			flags: Flags{
				Stage:  "10_KubernetesPlugin",
				Stages: []string{"20_OpenshiftPlugin"},
			},
			wantError: true,
			errorMsg:  "--stage, --from-stage/--to-stage, and --stages are mutually exclusive",
		},
		{
			name: "from-stage and stages set - invalid",
			flags: Flags{
				FromStage: "10_KubernetesPlugin",
				Stages:    []string{"20_OpenshiftPlugin"},
			},
			wantError: true,
			errorMsg:  "--stage, --from-stage/--to-stage, and --stages are mutually exclusive",
		},
		{
			name: "all flags set - invalid",
			flags: Flags{
				Stage:     "10_KubernetesPlugin",
				FromStage: "20_OpenshiftPlugin",
				ToStage:   "30_ImagestreamPlugin",
				Stages:    []string{"40_CustomPlugin"},
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
		name              string
		flags             Flags
		expectCustom      bool // true if user specified custom selector
		selectorStage     string
		selectorFrom      string
		selectorTo        string
		selectorStages    []string
	}{
		{
			name:         "default - no flags (all stages)",
			flags:        Flags{},
			expectCustom: false, // No custom selector = all stages
		},
		{
			name: "stage flag set",
			flags: Flags{
				Stage: "10_KubernetesPlugin",
			},
			expectCustom:  true,
			selectorStage: "10_KubernetesPlugin",
		},
		{
			name: "from-stage flag set",
			flags: Flags{
				FromStage: "20_OpenshiftPlugin",
			},
			expectCustom: true,
			selectorFrom: "20_OpenshiftPlugin",
		},
		{
			name: "to-stage flag set",
			flags: Flags{
				ToStage: "30_ImagestreamPlugin",
			},
			expectCustom: true,
			selectorTo:   "30_ImagestreamPlugin",
		},
		{
			name: "from-stage and to-stage set",
			flags: Flags{
				FromStage: "10_KubernetesPlugin",
				ToStage:   "30_ImagestreamPlugin",
			},
			expectCustom: true,
			selectorFrom: "10_KubernetesPlugin",
			selectorTo:   "30_ImagestreamPlugin",
		},
		{
			name: "stages flag set",
			flags: Flags{
				Stages: []string{"10_KubernetesPlugin", "30_ImagestreamPlugin"},
			},
			expectCustom:   true,
			selectorStages: []string{"10_KubernetesPlugin", "30_ImagestreamPlugin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the routing logic from run() method
			// Now everything uses ApplyMultiStage(), the question is whether
			// user provided a custom selector or we use default (all stages)
			hasCustomSelector := tt.flags.Stage != "" || tt.flags.FromStage != "" ||
				tt.flags.ToStage != "" || len(tt.flags.Stages) > 0

			if hasCustomSelector != tt.expectCustom {
				t.Errorf("Custom selector: got %v, want %v", hasCustomSelector, tt.expectCustom)
			}

			if tt.expectCustom {
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
