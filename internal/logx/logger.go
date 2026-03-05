package logx

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"litesync/internal/api"
)

type SLogger struct {
	base   *slog.Logger
	file   *os.File
	syncMu sync.Mutex
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

func NewWithFile(level string, logDir string) (*SLogger, string, error) {
	if strings.TrimSpace(logDir) == "" {
		return nil, "", api.Wrap(api.ErrInvalidArgument, "logDir is empty")
	}
	dir := filepath.Clean(logDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", api.Wrap(api.ErrPermissionDenied, "create log dir failed")
	}

	filename := "litesync-" + time.Now().Format("20060102") + ".log"
	path := filepath.Join(dir, filename)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, "", api.Wrap(api.ErrPermissionDenied, "open log file failed")
	}

	writer := io.MultiWriter(os.Stdout, file)
	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: parseLevel(level),
	})
	return &SLogger{
		base: slog.New(handler),
		file: file,
	}, path, nil
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
		kv = append(kv, "error_code", api.ErrorCode(err))
	}
	l.base.Error(msg, kv...)
}

func (l *SLogger) With(fields ...api.Field) api.Logger {
	return &SLogger{base: l.base.With(toKV(fields)...)}
}

func (l *SLogger) Sync() error {
	l.syncMu.Lock()
	defer l.syncMu.Unlock()

	if l.file != nil {
		if err := l.file.Sync(); err != nil {
			return err
		}
		err := l.file.Close()
		l.file = nil
		return err
	}
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
