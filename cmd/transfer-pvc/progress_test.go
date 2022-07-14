package transfer_pvc

import (
	"fmt"
	"testing"
)

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

func dataEqual(a, b *dataSize) (string, string, bool) {
	if a != nil && b != nil {
		if (a.val == b.val) && (a.unit == b.unit) {
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
			if a, b, e := dataEqual(got.TransferredData, tt.want.TransferredData); !e {
				t.Errorf("parseRsyncLogs() dataTransferred = %v, want %v", a, b)
			}
			if a, b, e := dataEqual(got.TransferRate, tt.want.TransferRate); !e {
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
