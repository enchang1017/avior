package logging

import (
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	t.Parallel()
	cases := map[string]slog.Level{
		"debug":    slog.LevelDebug,
		"DEBUG":    slog.LevelDebug,
		"  warn  ": slog.LevelWarn,
		"warning":  slog.LevelWarn,
		"error":    slog.LevelError,
		"info":     slog.LevelInfo,
		"":         slog.LevelInfo,
		"nonsense": slog.LevelInfo,
	}
	for input, want := range cases {
		if got := parseLevel(input); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestNewHonoursFormat(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"text", "TEXT", "json", ""} {
		if New("info", format) == nil {
			t.Errorf("New(info, %q) returned nil", format)
		}
	}
}
