package slogx

import (
	"log/slog"
	"os"
	"strings"
)

var sensitive = map[string]struct{}{
	"password":        {},
	"password_hash":   {},
	"refresh_token":   {},
	"access_token":    {},
	"jwt":             {},
	"secret":          {},
	"x-service-token": {},
	"authorization":   {},
	"cookie":          {},
}

func Init(service, level string) {
	lvl := parseLevel(level)
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if _, ok := sensitive[strings.ToLower(a.Key)]; ok {
				return slog.String(a.Key, "[REDACTED]")
			}
			return a
		},
	})
	logger := slog.New(h).With("service", service)
	slog.SetDefault(logger)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
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
