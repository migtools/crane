package transfer_pvc

import (
	"testing"
)

func Test_validateTransferCompletion(t *testing.T) {
	int100 := int64(100)
	exit0 := int32(0)
	exit23 := int32(23)

	tests := []struct {
		name    string
		p       *Progress
		wantErr bool
	}{
		{
			name: "succeeded",
			p: &Progress{
				ExitCode:           &exit0,
				TransferPercentage: &int100,
				TransferredData:    &dataSize{val: 10, unit: "M"},
			},
			wantErr: false,
		},
		{
			name: "failed",
			p: &Progress{
				ExitCode:         &exit23,
				TransferredFiles: 0,
				FailedFiles: []FailedFile{
					{Name: "/data/a", Err: "Permission denied (13)"},
				},
			},
			wantErr: true,
		},
		{
			name: "partially failed",
			p: &Progress{
				ExitCode:         &exit23,
				TransferredFiles: 1,
				TransferredData:  &dataSize{val: 1, unit: "M"},
				FailedFiles: []FailedFile{
					{Name: "/data/journal", Err: "Permission denied (13)"},
				},
			},
			wantErr: true,
		},
		{
			name:    "nil progress",
			p:       nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTransferCompletion(tt.p)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}
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
