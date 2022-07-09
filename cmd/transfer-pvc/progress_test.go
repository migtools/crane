package transfer_pvc

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

type fakeLogEmitter struct {
	stdout string
	stderr string
	r      io.ReadCloser
}

func (f fakeLogEmitter) Reader(p []byte) (n int, err error) {
	f.r = io.NopCloser(strings.NewReader(f.stdout))
	return f.r.Read(p)
}

func (f fakeLogEmitter) Close() {
	f.r.Close()
}

func Test_parseRsyncLogs(t *testing.T) {
	// int130 := int64(130)
	int6 := int64(6)
	tests := []struct {
		name    string
		stdout  string
		stderr  string
		want    progress
		wantErr bool
	}{
		{
			name: "failed-succeeded file list test",
			stdout: `16.78M   3%  105.06MB/s    0:00:00 (xfr#1, to-chk=19/21)2022/07/07 19:08:57 [480] <f+++++++++ admin.0
			33.55M   6%   86.40MB/s    0:00:00 (xfr#2, to-chk=18/21)2022/07/07 19:08:58 [480] <f+++++++++ admin.ns
			`,
			stderr: `rsync: [sender] send_files failed to open "/tmp/rsync-tests/a/dbvw": Permission denied (13)
			rsync: [sender] send_files failed to open "/tmp/rsync-tests/a/nhzmmmw": Permission denied (13)
			`,
			want: progress{
				files:       []string{"admin.0", "admin.ns"},
				failedFiles: []string{"/tmp/rsync-tests/a/dbvw", "/tmp/rsync-tests/a/nhzmmmw"},
				transferRate: &dataSize{
					size: float64(86.4),
					unit: "MB/s",
				},
				dataTransferred: &dataSize{
					size: float64(33.55),
					unit: "M",
				},
				transferPercentage: &int6,
			},
			wantErr: false,
		},
		// {
		// 	name: "file number stat test",
		// 	stdout: `Number of files: 136 (reg: 130, dir: 6)
		// 	Number of created files: 135 (reg: 130, dir: 5)
		// 	Number of deleted files: 0
		// 	Number of regular files transferred: 130
		// 	`,
		// 	want: progress{
		// 		totalFiles: &int130,
		// 		totalDirs:  &int6,
		// 	},
		// 	wantErr: false,
		// },
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intEqual := func(a, b *int64) (string, string, bool) {
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
			dataEqual := func(a, b *dataSize) (string, string, bool) {
				if a != nil && b != nil {
					if (a.size == b.size) && (a.unit == b.unit) {
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
			got, err := parseRsyncLogs(tt.stdout)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRsyncLogs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if a, b, e := intEqual(got.totalFiles, tt.want.totalFiles); !e {
				t.Errorf("parseRsyncLogs() totalFiles = %v, want %v", a, b)
			}
			if a, b, e := intEqual(got.totalDirs, tt.want.totalDirs); !e {
				t.Errorf("parseRsyncLogs() totalDirs = %v, want %v", a, b)
			}
			if a, b, e := intEqual(got.transferPercentage, tt.want.transferPercentage); !e {
				t.Errorf("parseRsyncLogs() percentage = %v, want %v", a, b)
			}
			if a, b, e := dataEqual(got.dataTransferred, tt.want.dataTransferred); !e {
				t.Errorf("parseRsyncLogs() dataTransferred = %v, want %v", a, b)
			}
			if a, b, e := dataEqual(got.transferRate, tt.want.transferRate); !e {
				t.Errorf("parseRsyncLogs() speed = %v, want %v", a, b)
			}
		})
	}
}

func Test_processRsyncLogs(t *testing.T) {
	tests := []struct {
		name    string
		r       io.ReadCloser
		want    string
		wantErr bool
	}{
		{
			name: "process logs",
			// r:    fakeLogEmitter{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := processRsyncLogs(tt.r)
			if (err != nil) != tt.wantErr {
				t.Errorf("processRsyncLogs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			b := new(bytes.Buffer)
			b.ReadFrom(got)
			gotOutput := b.String()
			if gotOutput != tt.want {
				t.Errorf("processRsyncLogs() = %v, want %v", gotOutput, tt.want)
			}
		})
	}
}
