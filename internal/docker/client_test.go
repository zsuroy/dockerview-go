package docker

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestSortPorts(t *testing.T) {
	ports := []PortMapping{
		{IP: "127.0.0.1", PrivatePort: 443, PublicPort: 8443, Type: "tcp"},
		{PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		{PrivatePort: 443, PublicPort: 8443, Type: "udp"},
		{PrivatePort: 80, Type: "tcp"},
		{IP: "0.0.0.0", PrivatePort: 443, PublicPort: 8443, Type: "tcp"},
	}
	want := []PortMapping{
		{PrivatePort: 80, Type: "tcp"},
		{PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		{IP: "0.0.0.0", PrivatePort: 443, PublicPort: 8443, Type: "tcp"},
		{IP: "127.0.0.1", PrivatePort: 443, PublicPort: 8443, Type: "tcp"},
		{PrivatePort: 443, PublicPort: 8443, Type: "udp"},
	}

	if got := sortPorts(ports); !reflect.DeepEqual(got, want) {
		t.Fatalf("sortPorts() = %#v, want %#v", got, want)
	}
}

func TestContainerOpUnknownOp(t *testing.T) {
	err := ContainerOp(context.Background(), nil, "abc123", "unknown")
	if err == nil {
		t.Error("expected error for unknown operation")
	}
	if err.Error() != "unknown operation: unknown" {
		t.Errorf("unexpected error: %v", err)
	}
}

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

func TestCappedBuffer(t *testing.T) {
	buf := newCappedBuffer(5)
	if _, err := buf.Write([]byte("hello!")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if buf.String() != "hello" {
		t.Fatalf("String() = %q, want %q", buf.String(), "hello")
	}
	if !buf.truncated {
		t.Fatal("expected truncated flag after exceeding limit")
	}
}

func TestParseStatsMemoryPercent(t *testing.T) {
	body := strings.NewReader(`{
		"cpu_stats": {
			"cpu_usage": { "total_usage": 200000000 },
			"system_cpu_usage": 1000000000,
			"online_cpus": 4
		},
		"precpu_stats": {
			"cpu_usage": { "total_usage": 100000000 },
			"system_cpu_usage": 900000000
		},
		"memory_stats": { "usage": 536870912, "limit": 1073741824 },
		"blkio_stats": { "io_service_bytes_recursive": [] },
		"networks": {}
	}`)

	cpu, memPct, memUsage, memLimit, _, _, err := parseStats(body)
	if err != nil {
		t.Fatalf("parseStats() error = %v", err)
	}
	if cpu <= 0 {
		t.Fatalf("cpu = %v, want > 0", cpu)
	}
	if memPct != 50 {
		t.Fatalf("memory percent = %v, want 50", memPct)
	}
	if memUsage != "512.0 MB" {
		t.Fatalf("memory usage = %q, want %q", memUsage, "512.0 MB")
	}
	if memLimit != 1073741824 {
		t.Fatalf("memory limit = %d, want %d", memLimit, 1073741824)
	}
}
