// Package logging builds the process logger.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New returns the process logger. Only boundary layers log; inner layers carry
// failures in their return values, so this is deliberately the only logger.
func New(level, format string) *slog.Logger {
	return slog.New(NewHandler(level, format))
}

// NewHandler returns the stdout half of the process logger.
//
// Separate from New because the telemetry package wraps this one to send the
// same records over OTLP as well, and it can only do that if it is handed the
// handler rather than the logger built around it.
func NewHandler(level, format string) slog.Handler {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	if strings.EqualFold(format, "text") {
		return slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.NewJSONHandler(os.Stdout, opts)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
