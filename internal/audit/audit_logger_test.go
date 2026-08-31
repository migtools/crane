package audit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// makeEntry builds a minimal logrus.Entry for use in Fire() calls.
func makeEntry(msg string, level logrus.Level) *logrus.Entry {
	return &logrus.Entry{
		Logger:  logrus.New(),
		Message: msg,
		Level:   level,
		Time:    time.Now(),
		Data:    logrus.Fields{},
	}
}

// --- FileHook tests ---

func TestNewFileHook(t *testing.T) {
	dir := t.TempDir()

	// Create a plain file so that MkdirAll fails when we try to use it as a directory.
	notADir := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(notADir, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid path creates file",
			path:    filepath.Join(dir, "audit", ".crane-audit.log"),
			wantErr: false,
		},
		{
			name:    "empty path returns error",
			path:    "",
			wantErr: true,
		},
		{
			name:    "bad path returns error when dir is a file",
			path:    filepath.Join(notADir, "audit.log"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook, err := NewFileHook(tt.path, nil)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hook != nil {
				if err := hook.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			}
			if !tt.wantErr {
				if _, statErr := os.Stat(tt.path); statErr != nil {
					t.Fatalf("file was not created at %s: %v", tt.path, statErr)
				}
			}
		})
	}
}

func TestNewFileHook_EnforcesPermissionsOnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.log")
	// Create file with permissive 0644 permissions
	if err := os.WriteFile(path, []byte("old content\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	hook, err := NewFileHook(path, nil)
	if err != nil {
		t.Fatalf("NewFileHook: %v", err)
	}
	if err := hook.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("permissions = %04o, want 0600", got)
	}
}

func TestFileHook_Fire_WritesJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	hook, err := NewFileHook(path, nil)
	if err != nil {
		t.Fatalf("NewFileHook: %v", err)
	}
	defer func() {
		if err := hook.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	if err := hook.Fire(makeEntry("test message", logrus.InfoLevel)); err != nil {
		t.Fatalf("Fire: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected file to contain data, got empty file")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data[:len(data)-1], &parsed); err != nil {
		t.Fatalf("file content is not valid JSON: %v\ncontent: %s", err, data)
	}
	if parsed["msg"] != "test message" {
		t.Errorf("msg = %q, want %q", parsed["msg"], "test message")
	}
	if parsed["level"] != "info" {
		t.Errorf("level = %q, want %q", parsed["level"], "info")
	}
}

func TestFileHook_AppendMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")

	for i, msg := range []string{"first", "second"} {
		hook, err := NewFileHook(path, nil)
		if err != nil {
			t.Fatalf("run %d NewFileHook: %v", i, err)
		}
		if err := hook.Fire(makeEntry(msg, logrus.InfoLevel)); err != nil {
			t.Fatalf("run %d Fire: %v", i, err)
		}
		if err := hook.Close(); err != nil {
			t.Fatalf("run %d Close: %v", i, err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (append mode), got %d:\n%s", len(lines), data)
	}
}

func TestFileHook_Close_PreventsFurtherWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	hook, err := NewFileHook(path, nil)
	if err != nil {
		t.Fatalf("NewFileHook: %v", err)
	}

	if err := hook.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := hook.Fire(makeEntry("after close", logrus.InfoLevel)); err == nil {
		t.Fatal("expected error when firing after Close, got nil")
	}
}

func TestFileHook_Levels_IncludesDebugExcludesTrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	hook, err := NewFileHook(path, nil)
	if err != nil {
		t.Fatalf("NewFileHook: %v", err)
	}
	defer func() {
		if err := hook.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	levels := hook.Levels()
	hasDebug, hasTrace := false, false
	for _, l := range levels {
		if l == logrus.DebugLevel {
			hasDebug = true
		}
		if l == logrus.TraceLevel {
			hasTrace = true
		}
	}
	if !hasDebug {
		t.Error("Levels() should include DebugLevel")
	}
	if hasTrace {
		t.Error("Levels() should not include TraceLevel (logger is set to DebugLevel)")
	}
}

func TestFileHook_Fire_InjectsCmdField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	cmd := "export"
	hook, err := NewFileHook(path, &cmd)
	if err != nil {
		t.Fatalf("NewFileHook: %v", err)
	}
	defer func() {
		if err := hook.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	entry := makeEntry("test", logrus.InfoLevel)
	if err := hook.Fire(entry); err != nil {
		t.Fatalf("Fire: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data[:len(data)-1], &parsed); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if parsed["cmd"] != "export" {
		t.Errorf("cmd = %q, want %q", parsed["cmd"], "export")
	}
}

func TestNewFileHook_CustomPath(t *testing.T) {
	dir := t.TempDir()
	customPath := filepath.Join(dir, "custom", "crane.jsonl")
	cmd := "export"
	hook, err := NewFileHook(customPath, &cmd)
	if err != nil {
		t.Fatalf("NewFileHook with custom path: %v", err)
	}
	defer func() {
		if err := hook.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	if err := hook.Fire(makeEntry("hello", logrus.InfoLevel)); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if _, err := os.Stat(customPath); err != nil {
		t.Fatalf("file not created at custom path %s: %v", customPath, err)
	}
}

// --- ConsoleHook tests ---

func TestNewConsoleHook_LevelsWithoutDebug(t *testing.T) {
	hook := NewConsoleHook(false)
	for _, l := range hook.Levels() {
		if l == logrus.DebugLevel || l == logrus.TraceLevel {
			t.Errorf("ConsoleHook without debug should not include level %s", l)
		}
	}
}

func TestNewConsoleHook_LevelsWithDebug(t *testing.T) {
	hook := NewConsoleHook(true)
	if len(hook.Levels()) != len(logrus.AllLevels) {
		t.Errorf("ConsoleHook with debug: got %d levels, want %d", len(hook.Levels()), len(logrus.AllLevels))
	}
}

func TestConsoleHook_Fire_WritesToWriter(t *testing.T) {
	var buf bytes.Buffer
	hook := &ConsoleHook{
		writer:    &buf,
		levels:    logrus.AllLevels,
		formatter: &logrus.TextFormatter{DisableColors: true},
	}

	if err := hook.Fire(makeEntry("hello console", logrus.InfoLevel)); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected output in writer, got nothing")
	}
	if !bytes.Contains(buf.Bytes(), []byte("hello console")) {
		t.Errorf("output does not contain message: %s", buf.Bytes())
	}
}
