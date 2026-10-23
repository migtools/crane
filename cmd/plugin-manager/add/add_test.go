package add

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestValidateFileInput(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name     string
		filename string
		wantErr  bool
	}{
		{name: "valid name", filename: "my-plugin", wantErr: false},
		{name: "empty name", filename: "", wantErr: true},
		{name: "dot", filename: ".", wantErr: true},
		{name: "dotdot", filename: "..", wantErr: true},
		{name: "forward slash", filename: "foo/bar", wantErr: true},
		{name: "back slash", filename: "foo\\bar", wantErr: true},
		{name: "traversal", filename: "../evil", wantErr: true},
		{name: "absolute path", filename: "/etc/passwd", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateFileInput(dir, tt.filename); (err != nil) != tt.wantErr {
				t.Errorf("validateFileInput(%q, %q) error = %v, wantErr %v", dir, tt.filename, err, tt.wantErr)
			}
		})
	}
}

func TestValidateFileInputExistingFile(t *testing.T) {
	dir := t.TempDir()
	filename := "existing-plugin"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := validateFileInput(dir, filename); err == nil {
		t.Errorf("expected error for existing file, got nil")
	}
}

func TestValidateFileInputExistingSymlink(t *testing.T) {
	dir := t.TempDir()
	filename := "broken-link"
	if err := os.Symlink(filepath.Join(dir, "does-not-exist"), filepath.Join(dir, filename)); err != nil {
		t.Fatal(err)
	}

	if err := validateFileInput(dir, filename); err == nil {
		t.Errorf("expected error for existing broken symlink, got nil")
	}
}

func TestDownloadBinaryHTTPResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    string
	}{
		{
			name:       "successful download",
			statusCode: http.StatusOK,
			body:       "plugin binary",
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			body:       "not found",
			wantErr:    "404 Not Found",
		},
		{
			name:       "internal server error",
			statusCode: http.StatusInternalServerError,
			body:       "internal server error",
			wantErr:    "500 Internal Server Error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			pluginDir := filepath.Join(t.TempDir(), "plugins")
			err := downloadBinary(pluginDir, "plugin", server.URL, logrus.New())
			pluginPath := filepath.Join(pluginDir, "plugin")

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("downloadBinary() returned nil error")
				}
				if !strings.Contains(err.Error(), server.URL) || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("downloadBinary() error = %q, want URL %q and status %q", err, server.URL, tt.wantErr)
				}
				if _, err := os.Stat(pluginPath); !os.IsNotExist(err) {
					t.Errorf("plugin file exists after failed download: %v", err)
				}
				if _, err := os.Stat(pluginDir); !os.IsNotExist(err) {
					t.Errorf("plugin directory exists after failed download: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("downloadBinary() returned error: %v", err)
			}
			contents, err := os.ReadFile(pluginPath)
			if err != nil {
				t.Fatalf("failed to read downloaded plugin: %v", err)
			}
			if string(contents) != tt.body {
				t.Errorf("downloaded plugin = %q, want %q", contents, tt.body)
			}
		})
	}
}
