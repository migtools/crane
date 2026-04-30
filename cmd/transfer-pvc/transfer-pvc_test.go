package transfer_pvc

import (
	"strings"
	"testing"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func Test_parseSourceDestinationMapping(t *testing.T) {
	tests := []struct {
		name            string
		mapping         string
		wantSource      string
		wantDestination string
		wantErr         bool
	}{
		{
			name:            "given a string with only source name, should return same values for both source and destination",
			mapping:         "validstring",
			wantSource:      "validstring",
			wantDestination: "validstring",
			wantErr:         false,
		},
		{
			name:            "given a string with a valid source to destination mapping, should return correct values for source and destination",
			mapping:         "source:destination",
			wantSource:      "source",
			wantDestination: "destination",
			wantErr:         false,
		},
		{
			name:            "given a string with invalid source to destination mapping, should return error",
			mapping:         "source::destination",
			wantSource:      "",
			wantDestination: "",
			wantErr:         true,
		},
		{
			name:            "given a string with empty destination name, should return error",
			mapping:         "source:",
			wantSource:      "",
			wantDestination: "",
			wantErr:         true,
		},
		{
			name:            "given a mapping with empty source and destination strings, should return error",
			mapping:         ":",
			wantSource:      "",
			wantDestination: "",
			wantErr:         true,
		},
		{
			name:            "given an empty string, should return error",
			mapping:         "",
			wantSource:      "",
			wantDestination: "",
			wantErr:         true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSource, gotDestination, err := parseSourceDestinationMapping(tt.mapping)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSourceDestinationMapping() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotSource != tt.wantSource {
				t.Errorf("parseSourceDestinationMapping() gotSource = %v, want %v", gotSource, tt.wantSource)
			}
			if gotDestination != tt.wantDestination {
				t.Errorf("parseSourceDestinationMapping() gotDestination = %v, want %v", gotDestination, tt.wantDestination)
			}
		})
	}
}

// Tests TransferPVCCommand.Validate for various error conditions and success scenarios
func TestTransferPVCCommand_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cmd     TransferPVCCommand
		wantErr bool
		errMsg  string
	}{
		{
			name: "source context nil returns error",
			cmd: TransferPVCCommand{
				sourceContext:      nil,
				destinationContext: &clientcmdapi.Context{Cluster: "dest-cluster"},
			},
			wantErr: true,
			errMsg:  "cannot evaluate source context",
		},
		{
			name: "destination context nil returns error",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "source-cluster"},
				destinationContext: nil,
			},
			wantErr: true,
			errMsg:  "cannot evaluate destination context",
		},
		{
			name: "same cluster for source and destination returns error",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "same-cluster"},
				destinationContext: &clientcmdapi.Context{Cluster: "same-cluster"},
			},
			wantErr: true,
			errMsg:  "both source and destination cluster are the same",
		},
		{
			name: "PVC validation failure cascades error",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "source-cluster"},
				destinationContext: &clientcmdapi.Context{Cluster: "dest-cluster"},
				Flags: Flags{
					PVC: PvcFlags{
						Name:      mappedNameVar{source: "", destination: "dest-pvc"},
						Namespace: mappedNameVar{source: "src-ns", destination: "dest-ns"},
					},
				},
			},
			wantErr: true,
			errMsg:  "source pvc name cannot be empty",
		},
		{
			name: "endpoint validation failure cascades error",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "source-cluster"},
				destinationContext: &clientcmdapi.Context{Cluster: "dest-cluster"},
				Flags: Flags{
					PVC: PvcFlags{
						Name:      mappedNameVar{source: "src-pvc", destination: "dest-pvc"},
						Namespace: mappedNameVar{source: "src-ns", destination: "dest-ns"},
					},
					Endpoint: EndpointFlags{
						Type:      endpointNginx,
						Subdomain: "",
					},
				},
			},
			wantErr: true,
			errMsg:  "subdomain cannot be empty when using nginx ingress",
		},
		{
			name: "validation succeeds with all required fields",
			cmd: TransferPVCCommand{
				sourceContext:      &clientcmdapi.Context{Cluster: "source-cluster"},
				destinationContext: &clientcmdapi.Context{Cluster: "dest-cluster"},
				Flags: Flags{
					PVC: PvcFlags{
						Name:      mappedNameVar{source: "src-pvc", destination: "dest-pvc"},
						Namespace: mappedNameVar{source: "src-ns", destination: "dest-ns"},
					},
					Endpoint: EndpointFlags{
						Type:      endpointNginx,
						Subdomain: "my.subdomain.example.com",
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cmd.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("TransferPVCCommand.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("TransferPVCCommand.Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

// Tests EndpointFlags.Validate for endpoint type validation
func TestEndpointFlags_Validate(t *testing.T) {
	tests := []struct {
		name    string
		flags   EndpointFlags
		wantErr bool
		errMsg  string
	}{
		{
			name: "empty type defaults to nginx and requires subdomain",
			flags: EndpointFlags{
				Type:      "",
				Subdomain: "",
			},
			wantErr: true,
			errMsg:  "subdomain cannot be empty when using nginx ingress",
		},
		{
			name: "nginx type missing subdomain returns error",
			flags: EndpointFlags{
				Type:      endpointNginx,
				Subdomain: "",
			},
			wantErr: true,
			errMsg:  "subdomain cannot be empty when using nginx ingress",
		},
		{
			name: "nginx type with subdomain passes validation",
			flags: EndpointFlags{
				Type:      endpointNginx,
				Subdomain: "my.subdomain.example.com",
			},
			wantErr: false,
		},
		{
			name: "route type does not require subdomain",
			flags: EndpointFlags{
				Type:      endpointRoute,
				Subdomain: "",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.flags.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("EndpointFlags.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("EndpointFlags.Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

// Tests PvcFlags.Validate for PVC name and namespace validation
func TestPvcFlags_Validate(t *testing.T) {
	tests := []struct {
		name    string
		flags   PvcFlags
		wantErr bool
		errMsg  string
	}{
		{
			name: "empty source name returns error",
			flags: PvcFlags{
				Name:      mappedNameVar{source: "", destination: "dest-pvc"},
				Namespace: mappedNameVar{source: "src-ns", destination: "dest-ns"},
			},
			wantErr: true,
			errMsg:  "source pvc name cannot be empty",
		},
		{
			name: "empty destination name returns error",
			flags: PvcFlags{
				Name:      mappedNameVar{source: "src-pvc", destination: ""},
				Namespace: mappedNameVar{source: "src-ns", destination: "dest-ns"},
			},
			wantErr: true,
			errMsg:  "destnation pvc name cannot be empty",
		},
		{
			name: "empty source namespace returns error",
			flags: PvcFlags{
				Name:      mappedNameVar{source: "src-pvc", destination: "dest-pvc"},
				Namespace: mappedNameVar{source: "", destination: "dest-ns"},
			},
			wantErr: true,
			errMsg:  "source pvc namespace cannot be empty",
		},
		{
			name: "empty destination namespace returns error",
			flags: PvcFlags{
				Name:      mappedNameVar{source: "src-pvc", destination: "dest-pvc"},
				Namespace: mappedNameVar{source: "src-ns", destination: ""},
			},
			wantErr: true,
			errMsg:  "destination pvc namespace cannot be empty",
		},
		{
			name: "all fields populated passes validation",
			flags: PvcFlags{
				Name:      mappedNameVar{source: "src-pvc", destination: "dest-pvc"},
				Namespace: mappedNameVar{source: "src-ns", destination: "dest-ns"},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.flags.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("PvcFlags.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("PvcFlags.Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestMappedNameVar_String(t *testing.T) {
	m := &mappedNameVar{source: "my-pvc", destination: "new-pvc"}
	got := m.String()
	want := "my-pvc:new-pvc"
	if got != want {
		t.Errorf("mappedNameVar.String() = %v, want %v", got, want)
	}
}

func TestMappedNameVar_Set(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantSource      string
		wantDestination string
		wantErr         bool
	}{
		{
			name:            "parses source:destination format",
			input:           "src-pvc:dest-pvc",
			wantSource:      "src-pvc",
			wantDestination: "dest-pvc",
			wantErr:         false,
		},
		{
			name:            "single value sets both source and destination",
			input:           "my-pvc",
			wantSource:      "my-pvc",
			wantDestination: "my-pvc",
			wantErr:         false,
		},
		{
			name:    "empty string returns error",
			input:   "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mappedNameVar{}
			err := m.Set(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("mappedNameVar.Set() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if m.source != tt.wantSource {
					t.Errorf("mappedNameVar.Set() source = %v, want %v", m.source, tt.wantSource)
				}
				if m.destination != tt.wantDestination {
					t.Errorf("mappedNameVar.Set() destination = %v, want %v", m.destination, tt.wantDestination)
				}
			}
		})
	}
}

func TestMappedNameVar_Type(t *testing.T) {
	m := &mappedNameVar{}
	got := m.Type()
	want := "string"
	if got != want {
		t.Errorf("mappedNameVar.Type() = %v, want %v", got, want)
	}
}

func TestQuantityVar_String(t *testing.T) {
	q := &quantityVar{}
	_ = q.Set("10Gi")
	got := q.String()
	if got != "10Gi" {
		t.Errorf("quantityVar.String() = %v, want %v", got, "10Gi")
	}
}

func TestQuantityVar_Set(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid quantity parses successfully",
			input:   "5Gi",
			wantErr: false,
		},
		{
			name:    "invalid quantity returns error",
			input:   "invalid",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &quantityVar{}
			err := q.Set(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("quantityVar.Set() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && q.quantity == nil {
				t.Errorf("quantityVar.Set() quantity should not be nil after successful set")
			}
		})
	}
}

func TestQuantityVar_Type(t *testing.T) {
	q := &quantityVar{}
	got := q.Type()
	want := "string"
	if got != want {
		t.Errorf("quantityVar.Type() = %v, want %v", got, want)
	}
}

func TestEndpointType_Set(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    endpointType
		wantErr bool
	}{
		{
			name:    "nginx-ingress is valid",
			input:   "nginx-ingress",
			want:    endpointNginx,
			wantErr: false,
		},
		{
			name:    "route is valid",
			input:   "route",
			want:    endpointRoute,
			wantErr: false,
		},
		{
			name:    "invalid type returns error",
			input:   "invalid-type",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var e endpointType
			err := e.Set(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("endpointType.Set() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && e != tt.want {
				t.Errorf("endpointType.Set() = %v, want %v", e, tt.want)
			}
		})
	}
}

func TestEndpointType_Type(t *testing.T) {
	e := endpointNginx
	got := e.Type()
	want := "string"
	if got != want {
		t.Errorf("endpointType.Type() = %v, want %v", got, want)
	}
}

