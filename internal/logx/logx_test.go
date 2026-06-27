package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNop(t *testing.T) {
	l := Nop()
	if l == nil {
		t.Fatal("Nop() returned nil")
	}
	// Nop should not panic on any method
	l.Debug("test")
	l.Info("test")
	l.Warn("test")
	l.Error("test")
	if err := l.Close(); err != nil {
		t.Errorf("Nop.Close() should return nil, got %v", err)
	}
}

func TestNew(t *testing.T) {
	l := New()
	if l == nil {
		t.Fatal("New() returned nil")
	}
	l.Debug("test new")
	l.Info("test new", "key", "value")
	l.Warn("test new")
	l.Error("test new", "err", "something")
	if err := l.Close(); err != nil {
		t.Errorf("New().Close() returned error: %v", err)
	}
}

func TestNewWithFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	l, err := NewWithFile(path, "debug")
	if err != nil {
		t.Fatalf("NewWithFile: %v", err)
	}
	l.Debug("debug msg", "k", "v")
	l.Info("info msg")
	l.Warn("warn msg")
	l.Error("error msg")

	if err := l.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	// Verify file was written
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	content := string(data)
	for _, expected := range []string{"debug msg", "info msg", "warn msg", "error msg"} {
		if !strings.Contains(content, expected) {
			t.Errorf("log file missing %q", expected)
		}
	}
}

func TestNewWithFileEmptyPath(t *testing.T) {
	l, err := NewWithFile("", "info")
	if err != nil {
		t.Fatalf("NewWithFile with empty path: %v", err)
	}
	l.Info("no file")
	if err := l.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNewWithFileInvalidDir(t *testing.T) {
	_, err := NewWithFile("/nonexistent/path/that/does/not/exist/test.log", "info")
	if err == nil {
		t.Error("expected error for invalid directory")
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected string // use slog.Level.String() for comparison
	}{
		{"debug", "DEBUG"},
		{"info", "INFO"},
		{"warn", "WARN"},
		{"error", "ERROR"},
		{"", "INFO"},
		{"unknown", "INFO"},
	}
	for _, tt := range tests {
		got := parseLevel(tt.input)
		if got.String() != tt.expected {
			t.Errorf("parseLevel(%q) = %s, want %s", tt.input, got.String(), tt.expected)
		}
	}
}

func TestFntoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, ""},
		{1, "1"},
		{-1, "-1"},
		{123, "123"},
		{-456, "-456"},
		{2147483647, "2147483647"},
		{-2147483648, "-2147483648"},
	}
	for _, tt := range tests {
		got := fntoa(tt.input)
		if got != tt.expected {
			t.Errorf("fntoa(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestAbs(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{0, 0},
		{5, 5},
		{-5, 5},
		{100, 100},
		{-100, 100},
	}
	for _, tt := range tests {
		got := abs(tt.input)
		if got != tt.expected {
			t.Errorf("abs(%d) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestCloseOnNop(t *testing.T) {
	l := Nop()
	if err := l.Close(); err != nil {
		t.Errorf("Nop.Close(): %v", err)
	}
}

func TestCloseOnNew(t *testing.T) {
	l := New()
	if err := l.Close(); err != nil {
		t.Errorf("New().Close(): %v", err)
	}
}

func TestLogLevelFiltering(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warn.log")

	l, err := NewWithFile(path, "warn")
	if err != nil {
		t.Fatalf("NewWithFile: %v", err)
	}
	l.Debug("should NOT appear")
	l.Info("should NOT appear")
	l.Warn("should appear")
	l.Error("should also appear")

	if err := l.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if strings.Contains(content, "should NOT appear") {
		t.Error("DEBUG/INFO messages should be filtered at WARN level")
	}
	if !strings.Contains(content, "should appear") {
		t.Error("WARN message missing")
	}
	if !strings.Contains(content, "should also appear") {
		t.Error("ERROR message missing")
	}
}

func TestLoggerLevelDebug(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "debug.log")

	l, err := NewWithFile(path, "debug")
	if err != nil {
		t.Fatalf("NewWithFile: %v", err)
	}
	l.Debug("debug visible")

	if err := l.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "debug visible") {
		t.Error("DEBUG message should be visible at DEBUG level")
	}
}

func TestLoggerSourceAttr(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.log")

	l, err := NewWithFile(path, "info")
	if err != nil {
		t.Fatalf("NewWithFile: %v", err)
	}
	l.Info("source test")

	if err := l.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "source") {
		t.Error("log output should contain source attribute")
	}
}