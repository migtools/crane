package transfer_pvc

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

// resetGlobals resets global variables between tests for isolation
func resetGlobals() {
	pastAttempts = Progress{}
	failedFiles = nil
}

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

func TestNewProgress(t *testing.T) {
	name := types.NamespacedName{Namespace: "test-ns", Name: "test-pvc"}
	p := NewProgress(name)

	if p.PVC != name {
		t.Errorf("NewProgress() PVC = %v, want %v", p.PVC, name)
	}
	if p.FailedFiles == nil || len(p.FailedFiles) != 0 {
		t.Errorf("NewProgress() FailedFiles should be empty slice")
	}
	if p.Errors == nil || len(p.Errors) != 0 {
		t.Errorf("NewProgress() Errors should be empty slice")
	}
	if p.startedAt.IsZero() {
		t.Errorf("NewProgress() startedAt should not be zero")
	}
}

func TestStatus(t *testing.T) {
	tests := []struct {
		name       string
		progress   Progress
		wantStatus status
	}{
		{
			name: "exit code 0 returns succeeded",
			progress: Progress{
				ExitCode: func() *int32 { c := int32(0); return &c }(),
			},
			wantStatus: succeeded,
		},
		{
			name: "exit code 23 with transferred files returns partially failed",
			progress: Progress{
				ExitCode:         func() *int32 { c := int32(23); return &c }(),
				TransferredFiles: 1,
				TransferredData:  &dataSize{val: 9, unit: "bytes"},
				TotalFiles:       func() *int64 { v := int64(1); return &v }(),
			},
			wantStatus: partiallyFailed,
		},
		{
			name: "exit code 23 with no data returns failed",
			progress: Progress{
				ExitCode:        func() *int32 { c := int32(23); return &c }(),
				TransferredData: &dataSize{val: 0, unit: "bytes"},
			},
			wantStatus: failed,
		},
		{
			name:       "nil exit code returns preparing",
			progress:   Progress{},
			wantStatus: preparing,
		},
		{
			name: "nil exit code with 100% returns finishing up",
			progress: Progress{
				TransferPercentage: func() *int64 { v := int64(100); return &v }(),
			},
			wantStatus: finishingUp,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.progress.Status()
			if got != tt.wantStatus {
				t.Errorf("Status() = %q, want %q", got, tt.wantStatus)
			}
		})
	}
}

func TestAsString_ErrorsDisplayed(t *testing.T) {
	tests := []struct {
		name           string
		progress       Progress
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "succeeded with no errors shows no error section",
			progress: Progress{
				ExitCode:  func() *int32 { c := int32(0); return &c }(),
				startedAt: time.Now(),
			},
			wantContains:   []string{"Succeeded"},
			wantNotContain: []string{"Failed files", "Errors"},
		},
		{
			name: "partially failed shows failed files",
			progress: Progress{
				ExitCode:         func() *int32 { c := int32(23); return &c }(),
				TransferredFiles: 1,
				TransferredData:  &dataSize{val: 9, unit: "bytes"},
				TotalFiles:       func() *int64 { v := int64(1); return &v }(),
				FailedFiles: []FailedFile{
					{Name: "/mnt/data/secret", Err: "Permission denied (13)"},
				},
				startedAt: time.Now(),
			},
			wantContains: []string{"Partially failed", "Failed files", "/mnt/data/secret", "Permission denied (13)"},
		},
		{
			name: "failed shows failed files",
			progress: Progress{
				ExitCode:        func() *int32 { c := int32(23); return &c }(),
				TransferredData: &dataSize{val: 0, unit: "bytes"},
				FailedFiles: []FailedFile{
					{Name: "/mnt/data/dir1", Err: "Permission denied (13)"},
					{Name: "/mnt/data/dir2", Err: "Permission denied (13)"},
				},
				startedAt: time.Now(),
			},
			wantContains: []string{"Failed", "Failed files", "/mnt/data/dir1", "/mnt/data/dir2"},
		},
		{
			name: "errors field displayed when completed (regression test for #286 shadowing fix)",
			progress: Progress{
				ExitCode:        func() *int32 { c := int32(1); return &c }(),
				TransferredData: &dataSize{val: 0, unit: "bytes"},
				Errors:          []string{"connection reset by peer"},
				startedAt:       time.Now(),
			},
			wantContains: []string{"Errors", "connection reset by peer"},
		},
		{
			name: "preparing status hides errors",
			progress: Progress{
				FailedFiles: []FailedFile{
					{Name: "/mnt/data/secret", Err: "Permission denied (13)"},
				},
				Errors:    []string{"some error"},
				startedAt: time.Now(),
			},
			wantContains:   []string{"Preparing"},
			wantNotContain: []string{"Failed files", "Errors"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outString, errString := tt.progress.AsString()
			combined := outString + errString

			for _, want := range tt.wantContains {
				if !strings.Contains(combined, want) {
					t.Errorf("output should contain %q, got:\n%s", want, combined)
				}
			}
			for _, notWant := range tt.wantNotContain {
				if strings.Contains(combined, notWant) {
					t.Errorf("output should NOT contain %q, got:\n%s", notWant, combined)
				}
			}
		})
	}
}

func TestStatus_NilTransferredData(t *testing.T) {
	exitCode := int32(1)
	p := Progress{
		ExitCode:         &exitCode,
		TransferredData:  nil,
		TransferredFiles: 0,
		TotalFiles:       nil,
		startedAt:        time.Now(),
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Status() panicked with nil TransferredData: %v", r)
		}
	}()

	s := p.Status()
	if s != failed {
		t.Errorf("Status() = %q, want %q", s, failed)
	}
}

func TestProgressMerge_BasicFields(t *testing.T) {
	resetGlobals()
	totalFiles := int64(150)
	p := &Progress{
		TransferredFiles: 10,
		TotalFiles:       nil,
		FailedFiles:      []FailedFile{},
		Errors:           []string{},
	}
	in := &Progress{
		TransferredFiles: 20,
		TotalFiles:       &totalFiles,
		FailedFiles:      []FailedFile{},
		Errors:           []string{},
	}
	p.Merge(in)
	if p.TransferredFiles != 30 {
		t.Errorf("Merge() TransferredFiles = %v, want 30", p.TransferredFiles)
	}
	if p.TotalFiles == nil || *p.TotalFiles != 150 {
		t.Errorf("Merge() TotalFiles = %v, want 150", p.TotalFiles)
	}
}

func TestProgressMerge_PercentageAggregation(t *testing.T) {
	resetGlobals()
	pct1 := int64(50)
	pct2 := int64(40)
	p := &Progress{
		TransferPercentage: &pct1,
		FailedFiles:        []FailedFile{},
		Errors:             []string{},
	}
	in := &Progress{
		TransferPercentage: &pct2,
		FailedFiles:        []FailedFile{},
		Errors:             []string{},
	}
	p.Merge(in)
	if p.TransferPercentage == nil {
		t.Errorf("Merge() TransferPercentage = nil, want non-nil")
	}
}

func TestProgressMerge_PercentageBasicUpdate(t *testing.T) {
	resetGlobals()
	inPct := int64(40)
	p := &Progress{
		TransferPercentage: nil,
		FailedFiles:        []FailedFile{},
		Errors:             []string{},
	}
	in := &Progress{
		TransferPercentage: &inPct,
		FailedFiles:        []FailedFile{},
		Errors:             []string{},
	}
	p.Merge(in)
	if p.TransferPercentage == nil {
		t.Errorf("Merge() TransferPercentage = nil, want 40")
		return
	}
	if *p.TransferPercentage != 40 {
		t.Errorf("Merge() TransferPercentage = %d, want 40", *p.TransferPercentage)
	}
}

func TestProgressMerge_PercentageAccumulationWithPastAttempts(t *testing.T) {
	resetGlobals()
	pastPct := int64(30)
	pastAttempts = Progress{
		TransferPercentage: &pastPct,
	}
	inPct := int64(20)
	p := &Progress{
		TransferPercentage: nil,
		FailedFiles:        []FailedFile{},
		Errors:             []string{},
	}
	in := &Progress{
		TransferPercentage: &inPct,
		FailedFiles:        []FailedFile{},
		Errors:             []string{},
	}
	p.Merge(in)
	if p.TransferPercentage == nil {
		t.Errorf("Merge() TransferPercentage = nil, want 50")
		return
	}
	if *p.TransferPercentage != 50 {
		t.Errorf("Merge() TransferPercentage = %d, want 50 (pastAttempts 30 + in 20)", *p.TransferPercentage)
	}
}

func TestProgressMerge_PercentageCapAt100(t *testing.T) {
	resetGlobals()
	pastPct := int64(80)
	pastAttempts = Progress{
		TransferPercentage: &pastPct,
	}
	pPct := int64(75)
	inPct := int64(30)
	p := &Progress{
		TransferPercentage: &pPct,
		FailedFiles:        []FailedFile{},
		Errors:             []string{},
	}
	in := &Progress{
		TransferPercentage: &inPct,
		FailedFiles:        []FailedFile{},
		Errors:             []string{},
	}
	p.Merge(in)
	if p.TransferPercentage == nil {
		t.Errorf("Merge() TransferPercentage = nil, want 75")
		return
	}
	if *p.TransferPercentage != 75 {
		t.Errorf("Merge() TransferPercentage = %d, want 75 (should not update when total > 100)", *p.TransferPercentage)
	}
}

func TestProgressMerge_PercentageOnlyUpdateIfHigher(t *testing.T) {
	resetGlobals()
	pastPct := int64(40)
	pastAttempts = Progress{
		TransferPercentage: &pastPct,
	}
	pPct := int64(40)
	inPct := int64(20)
	p := &Progress{
		TransferPercentage: &pPct,
		FailedFiles:        []FailedFile{},
		Errors:             []string{},
	}
	in := &Progress{
		TransferPercentage: &inPct,
		FailedFiles:        []FailedFile{},
		Errors:             []string{},
	}
	p.Merge(in)
	if p.TransferPercentage == nil {
		t.Errorf("Merge() TransferPercentage = nil, want 60")
		return
	}
	if *p.TransferPercentage != 60 {
		t.Errorf("Merge() TransferPercentage = %d, want 60 (pastAttempts 40 + in 20)", *p.TransferPercentage)
	}
}

func TestProgressMerge_PercentageDontUpdateIfLower(t *testing.T) {
	resetGlobals()
	pPct := int64(50)
	inPct := int64(30)
	p := &Progress{
		TransferPercentage: &pPct,
		FailedFiles:        []FailedFile{},
		Errors:             []string{},
	}
	in := &Progress{
		TransferPercentage: &inPct,
		FailedFiles:        []FailedFile{},
		Errors:             []string{},
	}
	p.Merge(in)
	if p.TransferPercentage == nil {
		t.Errorf("Merge() TransferPercentage = nil, want 50")
		return
	}
	if *p.TransferPercentage != 50 {
		t.Errorf("Merge() TransferPercentage = %d, want 50 (should not update when incoming is lower)", *p.TransferPercentage)
	}
}

func TestProgressMerge_PercentageResetWithRetries(t *testing.T) {
	resetGlobals()
	pPct := int64(60)
	inPct := int64(10)
	retryCount := 1
	p := &Progress{
		TransferPercentage: &pPct,
		TransferredFiles:   100,
		FailedFiles:        []FailedFile{},
		Errors:             []string{},
	}
	in := &Progress{
		TransferPercentage: &inPct,
		retries:            &retryCount,
		FailedFiles:        []FailedFile{},
		Errors:             []string{},
	}
	p.Merge(in)
	if pastAttempts.TransferPercentage == nil {
		t.Errorf("pastAttempts.TransferPercentage = nil after retry, want non-nil")
		return
	}
	if *pastAttempts.TransferPercentage != 60 {
		t.Errorf("pastAttempts.TransferPercentage = %d, want 60", *pastAttempts.TransferPercentage)
	}
	if pastAttempts.TransferredFiles != 100 {
		t.Errorf("pastAttempts.TransferredFiles = %d, want 100", pastAttempts.TransferredFiles)
	}
	if p.retries == nil || *p.retries != 1 {
		t.Errorf("p.retries should be 1 after merge")
	}
}

func TestProgressMerge_DataSizeAggregation(t *testing.T) {
	resetGlobals()
	pastAttempts = Progress{
		TransferredData: &dataSize{val: 100.0, unit: "M"},
	}
	p := &Progress{
		TransferredData: nil,
		FailedFiles:     []FailedFile{},
		Errors:          []string{},
	}
	in := &Progress{
		TransferredData: &dataSize{val: 50.0, unit: "M"},
		FailedFiles:     []FailedFile{},
		Errors:          []string{},
	}
	p.Merge(in)
	if p.TransferredData == nil {
		t.Errorf("Merge() TransferredData = nil, want non-nil")
		return
	}
	if p.TransferredData.val != 150.0 {
		t.Errorf("Merge() TransferredData.val = %v, want 150.0", p.TransferredData.val)
	}
}

func TestProgressMerge_ErrorAndFailedFileAppending(t *testing.T) {
	resetGlobals()
	p := &Progress{
		Errors:      []string{"error1"},
		FailedFiles: []FailedFile{{Name: "file1", Err: "err1"}},
	}
	in := &Progress{
		Errors:      []string{"error2", "error3"},
		FailedFiles: []FailedFile{{Name: "file2", Err: "err2"}, {Name: "file3", Err: "err3"}},
	}
	p.Merge(in)
	if len(p.Errors) != 3 {
		t.Errorf("Merge() len(Errors) = %v, want 3", len(p.Errors))
	}
	if len(p.FailedFiles) != 3 {
		t.Errorf("Merge() len(FailedFiles) = %v, want 3", len(p.FailedFiles))
	}
}
