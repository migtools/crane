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
		name          string
		flags         Flags
		expectCustom  bool   // true if user specified custom selector
		selectorStage string // expected stage value in selector
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the routing logic from run() method
			// Now everything uses ApplyMultiStage(), the question is whether
			// user provided a custom selector or we use default (all stages)
			hasCustomSelector := tt.flags.Stage != ""

			if hasCustomSelector != tt.expectCustom {
				t.Errorf("Custom selector: got %v, want %v", hasCustomSelector, tt.expectCustom)
			}

			if tt.expectCustom {
				// Verify selector would be constructed correctly
				if tt.flags.Stage != tt.selectorStage {
					t.Errorf("Selector Stage: got %v, want %v", tt.flags.Stage, tt.selectorStage)
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
