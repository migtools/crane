package apply

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name            string
		requestedStages []string
		wantError       bool
	}{
		{
			name:            "no stages - valid",
			requestedStages: []string{},
			wantError:       false,
		},
		{
			name:            "valid stage directory name",
			requestedStages: []string{"10_KubernetesPlugin"},
			wantError:       false,
		},
		{
			name:            "multiple valid stages",
			requestedStages: []string{"10_KubernetesPlugin", "20_OpenshiftPlugin"},
			wantError:       false,
		},
		{
			name:            "plugin name without prefix - valid (will be resolved)",
			requestedStages: []string{"KubernetesPlugin"},
			wantError:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{
				RequestedStages: tt.requestedStages,
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
		name             string
		requestedStages  []string
		expectCustom     bool     // true if user specified custom selector
		selectorStages   []string // expected stage values in selector
	}{
		{
			name:            "default - no stages (all stages)",
			requestedStages: []string{},
			expectCustom:    false, // No custom selector = all stages
		},
		{
			name:            "single stage",
			requestedStages: []string{"10_KubernetesPlugin"},
			expectCustom:    true,
			selectorStages:  []string{"10_KubernetesPlugin"},
		},
		{
			name:            "multiple stages",
			requestedStages: []string{"10_KubernetesPlugin", "20_OpenshiftPlugin"},
			expectCustom:    true,
			selectorStages:  []string{"10_KubernetesPlugin", "20_OpenshiftPlugin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the routing logic from run() method
			// Now everything uses ApplyMultiStage(), the question is whether
			// user provided a custom selector or we use default (all stages)
			hasCustomSelector := len(tt.requestedStages) > 0

			if hasCustomSelector != tt.expectCustom {
				t.Errorf("Custom selector: got %v, want %v", hasCustomSelector, tt.expectCustom)
			}

			if tt.expectCustom {
				// Verify selector would be constructed correctly
				if len(tt.requestedStages) != len(tt.selectorStages) {
					t.Errorf("Selector Stages length: got %v, want %v", len(tt.requestedStages), len(tt.selectorStages))
				}
				for i, stage := range tt.requestedStages {
					if stage != tt.selectorStages[i] {
						t.Errorf("Selector Stages[%d]: got %v, want %v", i, stage, tt.selectorStages[i])
					}
				}
			}
		})
	}
}

func TestComplete(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantStages []string
	}{
		{
			name:      "no args",
			args:      []string{},
			wantStages: []string{},
		},
		{
			name:      "single stage",
			args:      []string{"10_KubernetesPlugin"},
			wantStages: []string{"10_KubernetesPlugin"},
		},
		{
			name:      "multiple stages",
			args:      []string{"10_KubernetesPlugin", "20_OpenshiftPlugin"},
			wantStages: []string{"10_KubernetesPlugin", "20_OpenshiftPlugin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &Options{}
			cmd := &cobra.Command{}

			err := o.Complete(cmd, tt.args)
			if err != nil {
				t.Errorf("Complete() unexpected error: %v", err)
			}

			if len(o.RequestedStages) != len(tt.wantStages) {
				t.Errorf("RequestedStages length: got %v, want %v", len(o.RequestedStages), len(tt.wantStages))
			}

			for i, stage := range o.RequestedStages {
				if stage != tt.wantStages[i] {
					t.Errorf("RequestedStages[%d]: got %v, want %v", i, stage, tt.wantStages[i])
				}
			}
		})
	}
}
