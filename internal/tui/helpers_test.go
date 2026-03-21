package tui

import "testing"

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1K"},
		{1536, "2K"},           // 1.5KB rounds to 2K
		{1024 * 1024, "1M"},
		{int64(182 * 1024 * 1024), "182M"},
		{int64(1024 * 1024 * 1024), "1G"},
	}
	for _, tc := range tests {
		got := formatBytes(tc.input)
		if got != tc.expected {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0s"},
		{-5, "0s"},
		{45, "45s"},
		{60, "1m 0s"},
		{134, "2m 14s"},
		{3600, "1h 0m"},
		{8054, "2h 14m"},
	}
	for _, tc := range tests {
		got := formatUptime(tc.input)
		if got != tc.expected {
			t.Errorf("formatUptime(%d) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
