package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"litesync/internal/api"
)

type FileService struct {
	path string
	mu   sync.RWMutex
}

func NewFileService(path string) *FileService {
	return &FileService{path: filepath.Clean(path)}
}

func DefaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "windows", "darwin":
		return filepath.Join(base, "LiteSync", "config.yaml"), nil
	default:
		return filepath.Join(base, "litesync", "config.yaml"), nil
	}
}

func (s *FileService) Load(ctx context.Context) (api.Config, error) {
	select {
	case <-ctx.Done():
		return api.Config{}, ctx.Err()
	default:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		cfg := api.DefaultConfig()
		return cfg, nil
	}
	if err != nil {
		return api.Config{}, fmt.Errorf("%w: read config: %v", api.ErrIOTransient, err)
	}

	cfg := api.DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return api.Config{}, fmt.Errorf("%w: decode yaml: %v", api.ErrInvalidArgument, err)
	}
	cfg.ApplyDefaults()
	if err := s.Validate(cfg); err != nil {
		return api.Config{}, err
	}

	return cfg, nil
}

func (s *FileService) Save(ctx context.Context, cfg api.Config) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := s.Validate(cfg); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("%w: mkdir config dir: %v", api.ErrPermissionDenied, err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("%w: encode yaml: %v", api.ErrInternal, err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("%w: write temp config: %v", api.ErrIOTransient, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("%w: replace config: %v", api.ErrIOTransient, err)
	}
	return nil
}

func (s *FileService) Validate(cfg api.Config) error {
	if cfg.Version <= 0 {
		return api.Wrap(api.ErrInvalidArgument, "version must be greater than 0")
	}

	logLevel := strings.ToLower(cfg.App.LogLevel)
	switch logLevel {
	case "", "debug", "info", "warn", "error":
	default:
		return api.Wrap(api.ErrInvalidArgument, "app.log_level must be one of debug/info/warn/error")
	}

	runMode := strings.ToLower(cfg.App.RunMode)
	switch runMode {
	case "", "tray", "window":
	default:
		return api.Wrap(api.ErrInvalidArgument, "app.run_mode must be tray/window")
	}

	ids := make(map[api.JobID]struct{}, len(cfg.Jobs))
	targets := make(map[string]struct{}, len(cfg.Jobs))
	for i, job := range cfg.Jobs {
		if job.ID == "" {
			return api.Wrap(api.ErrInvalidArgument, fmt.Sprintf("jobs[%d].id is required", i))
		}
		if _, ok := ids[job.ID]; ok {
			return api.Wrap(api.ErrInvalidArgument, fmt.Sprintf("jobs[%d].id duplicated: %s", i, job.ID))
		}
		ids[job.ID] = struct{}{}

		if job.SourceDir == "" || job.TargetDir == "" {
			return api.Wrap(api.ErrInvalidArgument, fmt.Sprintf("jobs[%d] source_dir and target_dir are required", i))
		}
		if !filepath.IsAbs(job.SourceDir) {
			return api.Wrap(api.ErrInvalidArgument, fmt.Sprintf("jobs[%d].source_dir must be absolute", i))
		}
		if !filepath.IsAbs(job.TargetDir) {
			return api.Wrap(api.ErrInvalidArgument, fmt.Sprintf("jobs[%d].target_dir must be absolute", i))
		}

		src := filepath.Clean(job.SourceDir)
		dst := filepath.Clean(job.TargetDir)
		if equalPath(src, dst) {
			return api.Wrap(api.ErrInvalidArgument, fmt.Sprintf("jobs[%d] source_dir and target_dir cannot be the same", i))
		}
		if _, ok := targets[dst]; ok {
			return api.Wrap(api.ErrInvalidArgument, fmt.Sprintf("jobs[%d].target_dir duplicated: %s", i, dst))
		}
		targets[dst] = struct{}{}

		if job.Strategy.EventSync.DebounceMS < 0 {
			return api.Wrap(api.ErrInvalidArgument, fmt.Sprintf("jobs[%d].strategy.event_sync.debounce_ms must be >= 0", i))
		}
		if job.Strategy.PeriodicReconcile.IntervalMinutes < 0 {
			return api.Wrap(api.ErrInvalidArgument, fmt.Sprintf("jobs[%d].strategy.periodic_reconcile.interval_minutes must be >= 0", i))
		}
	}

	return nil
}

func (s *FileService) Watch(_ context.Context) (<-chan api.Config, error) {
	return nil, api.Wrap(api.ErrNotSupported, "config watch is not implemented yet")
}

func (s *FileService) Path() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.path
}

func equalPath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
