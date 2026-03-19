package docker

import (
	"testing"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    uint64
		expected string
	}{
		{"Bytes", 500, "500 B"},
		{"KB", 1024, "1.0 KB"},
		{"MB", 1024 * 1024, "1.0 MB"},
		{"GB", 1024 * 1024 * 1024, "1.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			if result != tt.expected {
				t.Errorf("formatBytes(%d) = %s, want %s", tt.bytes, result, tt.expected)
			}
		})
	}
}

func TestExtractContainerName(t *testing.T) {
	tests := []struct {
		name     string
		names    []string
		expected string
	}{
		{"Empty slice", []string{}, ""},
		{"Nil slice", nil, ""},
		{"Empty string in slice", []string{""}, ""},
		{"Name with leading slash", []string{"/my-container"}, "my-container"},
		{"Name without slash", []string{"my-container"}, "my-container"},
		{"Multiple names use first", []string{"/first", "/second"}, "first"},
		{"Complex name", []string{"/my-app_container_1"}, "my-app_container_1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractContainerName(tt.names)
			if result != tt.expected {
				t.Errorf("extractContainerName(%v) = %q, want %q", tt.names, result, tt.expected)
			}
		})
	}
}

func TestTruncateID(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		length   int
		expected string
	}{
		{"Full 64-char ID truncated to 12", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", 12, "abcdef123456"},
		{"Short ID returns as-is", "abc123", 12, "abc123"},
		{"Empty ID", "", 12, ""},
		{"Exact length", "abcdef123456", 12, "abcdef123456"},
		{"Truncate to 8", "abcdefgh12345678", 8, "abcdefgh"},
		{"Zero length", "abc123", 0, ""},
		{"ID shorter than requested length", "abc", 10, "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateID(tt.id, tt.length)
			if result != tt.expected {
				t.Errorf("truncateID(%q, %d) = %q, want %q", tt.id, tt.length, result, tt.expected)
			}
		})
	}
}
