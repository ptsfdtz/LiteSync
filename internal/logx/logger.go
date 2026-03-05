package logx

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"litesync/internal/api"
)

type SLogger struct {
	base *slog.Logger
}

func New(level string) *SLogger {
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(level),
	})
	return &SLogger{
		base: slog.New(handler),
	}
}

func NewWithWriter(level string, w io.Writer) *SLogger {
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: parseLevel(level),
	})
	return &SLogger{
		base: slog.New(handler),
	}
}

func (l *SLogger) Debug(msg string, fields ...api.Field) {
	l.base.Debug(msg, toKV(fields)...)
}

func (l *SLogger) Info(msg string, fields ...api.Field) {
	l.base.Info(msg, toKV(fields)...)
}

func (l *SLogger) Warn(msg string, fields ...api.Field) {
	l.base.Warn(msg, toKV(fields)...)
}

func (l *SLogger) Error(msg string, err error, fields ...api.Field) {
	kv := toKV(fields)
	if err != nil {
		kv = append(kv, "error", err.Error())
	}
	l.base.Error(msg, kv...)
}

func (l *SLogger) With(fields ...api.Field) api.Logger {
	return &SLogger{base: l.base.With(toKV(fields)...)}
}

func (l *SLogger) Sync() error {
	return nil
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func toKV(fields []api.Field) []any {
	kv := make([]any, 0, len(fields)*2)
	for _, f := range fields {
		kv = append(kv, f.Key, f.Value)
	}
	return kv
}
