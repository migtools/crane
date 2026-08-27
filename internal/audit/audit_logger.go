package audit

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

// FileHook writes every log entry as a JSON line to a file.
type FileHook struct {
	file      *os.File
	cmd       *string
	formatter logrus.Formatter
}

// NewFileHook opens (or creates) the file at path in append mode and returns a hook.
// cmd is a pointer to a string that is read on every Fire() call, so it can be set after the hook is created.
func NewFileHook(path string, cmd *string) (*FileHook, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &FileHook{
		file:      f,
		cmd:       cmd,
		formatter: &logrus.JSONFormatter{},
	}, nil
}

// Levels returns all log levels down to Debug. Trace is excluded because the
// configured logger is set to DebugLevel and will not emit Trace entries.
func (h *FileHook) Levels() []logrus.Level {
	return []logrus.Level{
		logrus.PanicLevel,
		logrus.FatalLevel,
		logrus.ErrorLevel,
		logrus.WarnLevel,
		logrus.InfoLevel,
		logrus.DebugLevel,
	}
}

// Fire is called by logrus for every log entry. It formats the entry as JSON and writes it to the file.
func (h *FileHook) Fire(entry *logrus.Entry) error {
	if h.cmd != nil && *h.cmd != "" {
		entry.Data["cmd"] = *h.cmd
	}
	line, err := h.formatter.Format(entry)
	if err != nil {
		return fmt.Errorf("audit file hook: format entry: %w", err)
	}
	if _, err := h.file.Write(line); err != nil {
		return fmt.Errorf("audit file hook: write to %s: %w", h.file.Name(), err)
	}
	return nil
}

// Close closes the underlying file. Call this when the program exits.
func (h *FileHook) Close() error {
	return h.file.Close()
}

// ConsoleHook writes log entries to stderr, filtered by the levels it is given.
// This replaces logrus's default output so the level list can be controlled independently of the file hook.
type ConsoleHook struct {
	writer    io.Writer
	levels    []logrus.Level
	formatter logrus.Formatter
}

// NewConsoleHook returns a hook that writes to stderr.
// If debug is true, all levels are shown; otherwise only Info and above.
func NewConsoleHook(debug bool) *ConsoleHook {
	levels := []logrus.Level{
		logrus.PanicLevel,
		logrus.FatalLevel,
		logrus.ErrorLevel,
		logrus.WarnLevel,
		logrus.InfoLevel,
	}
	if debug {
		levels = logrus.AllLevels
	}
	return &ConsoleHook{
		writer:    os.Stderr,
		levels:    levels,
		formatter: &logrus.TextFormatter{},
	}
}

func (h *ConsoleHook) Levels() []logrus.Level {
	return h.levels
}

func (h *ConsoleHook) Fire(entry *logrus.Entry) error {
	line, err := h.formatter.Format(entry)
	if err != nil {
		return fmt.Errorf("audit console hook: format entry: %w", err)
	}
	if _, err := h.writer.Write(line); err != nil {
		return fmt.Errorf("audit console hook: write to stderr: %w", err)
	}
	return nil
}
