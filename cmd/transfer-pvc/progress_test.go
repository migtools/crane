package transfer_pvc

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

// Test helpers

func intEqual(a, b *int64) (string, string, bool) {
	if a != nil && b != nil && *a == *b {
		return fmt.Sprintf("%d", a), fmt.Sprintf("%d", b), true
	}
	as, bs := "nil", "nil"
	if a != nil {
		as = fmt.Sprintf("%d", *a)
	}
	if b != nil {
		bs = fmt.Sprintf("%d", *b)
	}
	return as, bs, a == b
}

func dataEqual(a, b *dataSize, tolerance float64) (string, string, bool) {
	if a != nil && b != nil {
		if math.Abs(a.val-b.val) <= tolerance && a.unit == b.unit {
			return a.String(), b.String(), true
		}

		return a.String(), b.String(), false
	}
	as, bs := "nil", "nil"
	if a != nil {
		as = a.String()
	}
	if b != nil {
		bs = b.String()
	}
	return as, bs, a == b
}

func Test_parseRsyncLogs(t *testing.T) {
	int130 := int64(130)
	int6 := int64(6)
	int49 := int64(49948)
	tests := []struct {
		name            string
		stdout          string
		stderr          string
		want            Progress
		wantStatus      status
		wantUnProcessed string
	}{
		{
			name: "failed-succeeded file list test",
			stdout: `
16.78M   3%  105.06MB/s    0:00:00 (xfr#1, to-chk=19/21)
admin.0
33.55M   6%   86.40MB/s    0:00:00 (xfr#2, to-chk=18/21)
admin.ns
`,
			stderr: `
rsync: [sender] send_files failed to open "/tmp/rsync-tests/a/dbvw": Permission denied (13)
rsync: [sender] send_files failed to open "/tmp/rsync-tests/a/nhzmmmw": Permission denied (13)`,
			want: Progress{
				FailedFiles: []FailedFile{
					{Name: "/tmp/rsync-tests/a/dbvw", Err: "Permission denied (13)"},
					{Name: "/tmp/rsync-tests/a/nhzmmmw", Err: "Permission denied (13)"},
				},
				TransferRate: &dataSize{
					val:  float64(86.4),
					unit: "MB/s",
				},
				TransferredData: &dataSize{
					val:  float64(33.55),
					unit: "M",
				},
				TransferPercentage: &int6,
			},
			wantStatus:      transferInProgress,
			wantUnProcessed: "",
		},
		{
			name: "file number stat test",
			stdout: `
Number of files: 136 (reg: 130, dir: 6)
Number of created files: 135 (reg: 130, dir: 5)
Number of deleted files: 0
Number of regular files transferred: 130
`,
			want: Progress{
				TotalFiles:       &int130,
				TransferredFiles: 130,
			},
			wantStatus:      preparing,
			wantUnProcessed: "",
		},
		{
			name: "unprocessed line test",
			stdout: `
Number of files: 136 (reg: 130, dir: 6)
Number of created files: 135 (reg: 130, dir: 5)
Number of deleted files: 0
Number of re`,
			want: Progress{
				TotalFiles: &int130,
			},
			wantStatus:      preparing,
			wantUnProcessed: "Number of re",
		},
		{
			name: "final stats",
			stdout: `
2022/07/14 18:09:11 [549] Number of files: 49,961 (reg: 49,948, dir: 13)
Number of files: 49,961 (reg: 49,948, dir: 13)
2022/07/14 18:09:11 [549] Number of created files: 49,959 (reg: 49,948, dir: 11)
Number of created files: 49,959 (reg: 49,948, dir: 11)
2022/07/14 18:09:11 [549] Number of deleted files: 0
Number of deleted files: 0
2022/07/14 18:09:11 [549] Number of regular files transferred: 49,948
Number of regular files transferred: 49,948
2022/07/14 18:09:11 [549] Total file size: 8.67G bytes
Total file size: 8.67G bytes
2022/07/14 18:09:11 [549] Total transferred file size: 8.67G bytes
Total transferred file size: 8.67G bytes
2022/07/14 18:09:11 [549] Literal data: 8.67G bytes
Literal data: 8.67G bytes
2022/07/14 18:09:11 [549] Matched data: 0 bytes
Matched data: 0 bytes
2022/07/14 18:09:11 [549] File list size: 655.34K
File list size: 655.34K
2022/07/14 18:09:11 [549] File list generation time: 0.138 seconds
File list generation time: 0.138 seconds
2022/07/14 18:09:11 [549] File list transfer time: 0.000 seconds
File list transfer time: 0.000 seconds
2022/07/14 18:09:11 [549] Total bytes sent: 8.67G
Total bytes sent: 8.67G
2022/07/14 18:09:11 [549] Total bytes received: 12.79M
Total bytes received: 12.79M

2022/07/14 18:09:11 [549] sent 8.67G bytes  received 12.79M bytes  88.19M bytes/sec
sent 8.67G bytes  received 12.79M bytes  88.19M bytes/sec
2022/07/14 18:09:11 [549] total size is 8.67G  speedup is 1.00
total size is 8.67G  speedup is 1.00`,
			want: Progress{
				TotalFiles: &int49,
				TransferredData: &dataSize{
					val:  float64(8.67),
					unit: "G",
				},
				TransferredFiles: 49948,
			},
			wantStatus:      preparing,
			wantUnProcessed: "total size is 8.67G  speedup is 1.00",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, unprocessed := parseRsyncLogs(tt.stdout)
			if a, b, e := intEqual(got.TotalFiles, tt.want.TotalFiles); !e {
				t.Errorf("parseRsyncLogs() totalFiles = %v, want %v", a, b)
			}
			if a, b, e := intEqual(got.TransferPercentage, tt.want.TransferPercentage); !e {
				t.Errorf("parseRsyncLogs() percentage = %v, want %v", a, b)
			}
			if a, b, e := dataEqual(got.TransferredData, tt.want.TransferredData, 0.0); !e {
				t.Errorf("parseRsyncLogs() dataTransferred = %v, want %v", a, b)
			}
			if a, b, e := dataEqual(got.TransferRate, tt.want.TransferRate, 0.0); !e {
				t.Errorf("parseRsyncLogs() speed = %v, want %v", a, b)
			}
			if got.TransferredFiles != tt.want.TransferredFiles {
				t.Errorf("parseRsyncLogs() transferredFiles = %d, want %d", got.TransferredFiles, tt.want.TransferredFiles)
			}
			if got.Status() != tt.wantStatus {
				t.Errorf("parseRsyncLogs() status = %v, want %v", got.Status(), tt.wantStatus)
			}
			if unprocessed != tt.wantUnProcessed {
				t.Errorf("parseRsyncLogs() unprocessed = %v, want %v", unprocessed, tt.wantUnProcessed)
			}
		})
	}
}

func TestNewDataSize_ParseWithUnit(t *testing.T) {
	tests := []struct {
		name     string
		str      string
		wantVal  float64
		wantUnit string
	}{
		{"with M unit", "100.5M", 100.5, "M"},
		{"with G unit", "1.5G", 1.5, "G"},
		{"with K unit", "512K", 512, "K"},
		{"with T unit", "2T", 2, "T"},
		{"with MB/s unit", "88.19MB/s", 88.19, "MB/s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newDataSize(tt.str)
			if got == nil {
				t.Errorf("newDataSize(%q) = nil, want non-nil", tt.str)
				return
			}
			if got.val != tt.wantVal {
				t.Errorf("newDataSize(%q).val = %v, want %v", tt.str, got.val, tt.wantVal)
			}
			if got.unit != tt.wantUnit {
				t.Errorf("newDataSize(%q).unit = %v, want %v", tt.str, got.unit, tt.wantUnit)
			}
		})
	}
}

func TestNewDataSize_ParseWithoutUnit(t *testing.T) {
	got := newDataSize("1024")
	if got == nil {
		t.Errorf("newDataSize('1024') = nil, want non-nil")
		return
	}
	if got.val != 1024 {
		t.Errorf("newDataSize('1024').val = %v, want 1024", got.val)
	}
	if got.unit != "bytes" {
		t.Errorf("newDataSize('1024').unit = %v, want 'bytes'", got.unit)
	}
}

func TestNewDataSize_ParseDecimalValues(t *testing.T) {
	tests := []struct {
		name     string
		str      string
		wantVal  float64
		wantUnit string
	}{
		{"0.5G", "0.5G", 0.5, "G"},
		{"12.34T", "12.34T", 12.34, "T"},
		{"88.19MB/s", "88.19MB/s", 88.19, "MB/s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newDataSize(tt.str)
			if got == nil {
				t.Errorf("newDataSize(%q) = nil, want non-nil", tt.str)
				return
			}
			if got.val != tt.wantVal {
				t.Errorf("newDataSize(%q).val = %v, want %v", tt.str, got.val, tt.wantVal)
			}
			if got.unit != tt.wantUnit {
				t.Errorf("newDataSize(%q).unit = %v, want %v", tt.str, got.unit, tt.wantUnit)
			}
		})
	}
}

func TestAddDataSize_SameUnit(t *testing.T) {
	tests := []struct {
		name string
		a    *dataSize
		b    *dataSize
		want *dataSize
	}{
		{
			name: "same unit M",
			a:    &dataSize{val: 100.5, unit: "M"},
			b:    &dataSize{val: 50.25, unit: "M"},
			want: &dataSize{val: 150.75, unit: "M"},
		},
		{
			name: "same unit G",
			a:    &dataSize{val: 1.0, unit: "G"},
			b:    &dataSize{val: 2.5, unit: "G"},
			want: &dataSize{val: 3.5, unit: "G"},
		},
		{
			name: "same unit bytes",
			a:    &dataSize{val: 1024, unit: "bytes"},
			b:    &dataSize{val: 512, unit: "bytes"},
			want: &dataSize{val: 1536, unit: "bytes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addDataSize(tt.a, tt.b)
			if a, b, e := dataEqual(got, tt.want, 0.0); !e {
				t.Errorf("addDataSize() = %v, want %v", a, b)
			}
		})
	}
}

func TestAddDataSize_DifferentUnits(t *testing.T) {
	tests := []struct {
		name string
		a    *dataSize
		b    *dataSize
		want *dataSize
	}{
		{
			name: "G + M results in G",
			a:    &dataSize{val: 1.0, unit: "G"},
			b:    &dataSize{val: 500.0, unit: "M"},
			want: &dataSize{val: 1.5, unit: "G"},
		},
		{
			name: "M + G results in G",
			a:    &dataSize{val: 500.0, unit: "M"},
			b:    &dataSize{val: 1.0, unit: "G"},
			want: &dataSize{val: 1.5, unit: "G"},
		},
		{
			name: "K + M results in M",
			a:    &dataSize{val: 1000.0, unit: "K"},
			b:    &dataSize{val: 1.0, unit: "M"},
			want: &dataSize{val: 2.0, unit: "M"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addDataSize(tt.a, tt.b)
			if a, b, e := dataEqual(got, tt.want, 0.001); !e {
				t.Errorf("addDataSize() = %v, want %v", a, b)
			}
		})
	}
}

func TestAddDataSize_NilInput(t *testing.T) {
	a := &dataSize{val: 100.0, unit: "M"}
	got := addDataSize(a, nil)
	if got != nil {
		t.Errorf("addDataSize() with nil b = %v, want nil", got)
	}
}

func TestAddDataSize_ZeroValues(t *testing.T) {
	a := &dataSize{val: 0.0, unit: "M"}
	b := &dataSize{val: 100.0, unit: "M"}
	got := addDataSize(a, b)
	want := &dataSize{val: 100.0, unit: "M"}
	if a, b, e := dataEqual(got, want, 0.0); !e {
		t.Errorf("addDataSize() = %v, want %v", a, b)
	}
}

func TestDataSizeString(t *testing.T) {
	tests := []struct {
		name string
		ds   *dataSize
		want string
	}{
		{"100.50 M", &dataSize{val: 100.5, unit: "M"}, "100.50 M"},
		{"1.00 G", &dataSize{val: 1.0, unit: "G"}, "1.00 G"},
		{"88.19 MB/s", &dataSize{val: 88.19, unit: "MB/s"}, "88.19 MB/s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ds.String(); got != tt.want {
				t.Errorf("dataSize.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDataSizeMarshalJSON_VariousUnits(t *testing.T) {
	tests := []struct {
		name    string
		ds      *dataSize
		wantStr string
	}{
		{"100.5 M", &dataSize{val: 100.5, unit: "M"}, "100.50 M"},
		{"1.23 G", &dataSize{val: 1.23, unit: "G"}, "1.23 G"},
		{"88.19 MB/s", &dataSize{val: 88.19, unit: "MB/s"}, "88.19 MB/s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.ds.MarshalJSON()
			if err != nil {
				t.Errorf("MarshalJSON() error = %v", err)
				return
			}
			var gotStr string
			if err := json.Unmarshal(got, &gotStr); err != nil {
				t.Errorf("Unmarshal error = %v", err)
				return
			}
			if gotStr != tt.wantStr {
				t.Errorf("MarshalJSON() = %v, want %v", gotStr, tt.wantStr)
			}
		})
	}
}
