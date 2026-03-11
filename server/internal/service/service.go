package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"litesync/server/internal/backup"
	"litesync/server/internal/config"
	"litesync/server/internal/logs"
	"litesync/server/internal/model"
	"litesync/server/internal/watcher"
)

const (
	maxLogEntries = 500
	maxInterval   = 7 * 24 * 60
)

var ErrBackupAlreadyRunning = errors.New("backup is already running")

type Service struct {
	baseCtx    context.Context
	baseCancel context.CancelFunc

	store     *config.Store
	logBuffer *logs.Buffer

	cfgMu  sync.RWMutex
	config model.Config

	statusMu sync.RWMutex
	status   model.RuntimeStatus

	runnerMu     sync.Mutex
	runnerCancel context.CancelFunc
	waitGroup    sync.WaitGroup
}

func New(dataDir string) (*Service, error) {
	store := config.NewStore(dataDir)
	cfg, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	baseCtx, baseCancel := context.WithCancel(context.Background())

	s := &Service{
		baseCtx:    baseCtx,
		baseCancel: baseCancel,
		store:      store,
		logBuffer:  logs.NewBuffer(maxLogEntries),
		config:     cfg,
		status: model.RuntimeStatus{
			CurrentAction: "idle",
		},
	}

	s.logInfo("service initialized")
	s.restartRunners()

	return s, nil
}

func (s *Service) Shutdown() {
	s.baseCancel()

	s.runnerMu.Lock()
	if s.runnerCancel != nil {
		s.runnerCancel()
		s.runnerCancel = nil
	}
	s.runnerMu.Unlock()

	s.waitGroup.Wait()
}

func (s *Service) GetConfig() model.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.config
}

func (s *Service) UpdateConfig(cfg model.Config) error {
	cfg = sanitizeConfig(cfg)
	if err := validateConfig(cfg); err != nil {
		return err
	}

	if err := s.store.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	s.cfgMu.Lock()
	s.config = cfg
	s.cfgMu.Unlock()

	s.logInfo("configuration updated")
	s.restartRunners()
	return nil
}

func (s *Service) GetStatus() model.RuntimeStatus {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	return s.status
}

func (s *Service) GetLogs(limit int) []model.LogEntry {
	return s.logBuffer.List(limit)
}

func (s *Service) TriggerBackup(ctx context.Context, trigger string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(trigger) == "" {
		trigger = "manual"
	}

	cfg := s.GetConfig()
	if err := validateRuntimeConfig(cfg); err != nil {
		return err
	}

	if !s.beginRun(trigger) {
		return ErrBackupAlreadyRunning
	}

	s.logInfo(fmt.Sprintf("backup started by %s", trigger))
	result, err := backup.Run(ctx, cfg.SourceDir, cfg.TargetDir)
	if err != nil {
		s.completeRun(err, "")
		s.logError(fmt.Sprintf("backup failed: %v", err))
		return err
	}

	s.completeRun(nil, result.Destination)
	s.logInfo(fmt.Sprintf("backup finished: %d files, %d bytes -> %s", result.FilesCopied, result.BytesCopied, result.Destination))
	return nil
}

func (s *Service) beginRun(trigger string) bool {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()

	if s.status.Running {
		return false
	}

	now := time.Now().UTC()
	s.status.Running = true
	s.status.CurrentAction = "running backup"
	s.status.LastRunAt = &now
	s.status.LastTrigger = trigger
	s.status.TotalRuns++
	return true
}

func (s *Service) completeRun(runErr error, destination string) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()

	s.status.Running = false
	s.status.CurrentAction = "idle"

	if runErr != nil {
		s.status.FailedRuns++
		s.status.LastError = runErr.Error()
		return
	}

	now := time.Now().UTC()
	s.status.SuccessRuns++
	s.status.LastSuccessAt = &now
	s.status.LastError = ""
	if destination != "" {
		s.status.CurrentAction = "idle (last: " + destination + ")"
	}
}

func (s *Service) restartRunners() {
	s.runnerMu.Lock()
	defer s.runnerMu.Unlock()

	if s.runnerCancel != nil {
		s.runnerCancel()
		s.runnerCancel = nil
	}

	cfg := s.GetConfig()
	runnerCtx, runnerCancel := context.WithCancel(s.baseCtx)
	s.runnerCancel = runnerCancel

	if cfg.IntervalMinutes > 0 && cfg.SourceDir != "" && cfg.TargetDir != "" {
		interval := time.Duration(cfg.IntervalMinutes) * time.Minute
		s.setNextScheduledRun(time.Now().UTC().Add(interval))
		s.waitGroup.Add(1)
		go func() {
			defer s.waitGroup.Done()
			s.runScheduler(runnerCtx, interval)
		}()
		s.logInfo(fmt.Sprintf("scheduler enabled: every %d minute(s)", cfg.IntervalMinutes))
	} else {
		s.clearNextScheduledRun()
		s.logInfo("scheduler disabled until source/target are configured")
	}

	if cfg.WatchChanges && cfg.SourceDir != "" {
		s.waitGroup.Add(1)
		go func() {
			defer s.waitGroup.Done()
			s.runWatcher(runnerCtx, cfg.SourceDir)
		}()
		s.logInfo("file watcher enabled")
	} else {
		s.logInfo("file watcher disabled")
	}
}

func (s *Service) runScheduler(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.setNextScheduledRun(time.Now().UTC().Add(interval))
			if err := s.TriggerBackup(ctx, "schedule"); err != nil && !errors.Is(err, ErrBackupAlreadyRunning) {
				s.logError(fmt.Sprintf("scheduled backup failed: %v", err))
			}
		}
	}
}

func (s *Service) runWatcher(ctx context.Context, sourceDir string) {
	w := watcher.New(2 * time.Second)
	err := w.Run(ctx, sourceDir, func() {
		if triggerErr := s.TriggerBackup(ctx, "watch"); triggerErr != nil && !errors.Is(triggerErr, ErrBackupAlreadyRunning) {
			s.logError(fmt.Sprintf("watch-triggered backup failed: %v", triggerErr))
		}
	})
	if err != nil && ctx.Err() == nil {
		s.logError(fmt.Sprintf("watcher stopped unexpectedly: %v", err))
	}
}

func (s *Service) setNextScheduledRun(next time.Time) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()

	nextCopy := next
	s.status.NextScheduledRun = &nextCopy
}

func (s *Service) clearNextScheduledRun() {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.status.NextScheduledRun = nil
}

func (s *Service) logInfo(message string) {
	s.logBuffer.Add("info", message)
}

func (s *Service) logError(message string) {
	s.logBuffer.Add("error", message)
}

func sanitizeConfig(cfg model.Config) model.Config {
	cfg.SourceDir = filepath.Clean(strings.TrimSpace(cfg.SourceDir))
	cfg.TargetDir = filepath.Clean(strings.TrimSpace(cfg.TargetDir))
	if cfg.SourceDir == "." {
		cfg.SourceDir = ""
	}
	if cfg.TargetDir == "." {
		cfg.TargetDir = ""
	}
	return cfg
}

func validateConfig(cfg model.Config) error {
	if cfg.SourceDir == "" {
		return errors.New("sourceDir is required")
	}
	if cfg.TargetDir == "" {
		return errors.New("targetDir is required")
	}
	if cfg.IntervalMinutes < 1 || cfg.IntervalMinutes > maxInterval {
		return fmt.Errorf("intervalMinutes must be between 1 and %d", maxInterval)
	}

	sourceAbs, err := filepath.Abs(cfg.SourceDir)
	if err != nil {
		return fmt.Errorf("invalid sourceDir: %w", err)
	}
	targetAbs, err := filepath.Abs(cfg.TargetDir)
	if err != nil {
		return fmt.Errorf("invalid targetDir: %w", err)
	}
	if samePath(sourceAbs, targetAbs) {
		return errors.New("sourceDir and targetDir must be different")
	}
	if isSubPath(sourceAbs, targetAbs) {
		return errors.New("targetDir cannot be inside sourceDir")
	}

	sourceInfo, err := os.Stat(sourceAbs)
	if err != nil {
		return fmt.Errorf("sourceDir does not exist: %w", err)
	}
	if !sourceInfo.IsDir() {
		return errors.New("sourceDir must be a directory")
	}

	if err := os.MkdirAll(targetAbs, 0o755); err != nil {
		return fmt.Errorf("targetDir is not writable: %w", err)
	}

	return nil
}

func validateRuntimeConfig(cfg model.Config) error {
	if cfg.SourceDir == "" || cfg.TargetDir == "" {
		return errors.New("sourceDir and targetDir must be configured")
	}

	return nil
}

func samePath(pathA string, pathB string) bool {
	return strings.EqualFold(filepath.Clean(pathA), filepath.Clean(pathB))
}

func isSubPath(basePath string, candidatePath string) bool {
	base := strings.ToLower(filepath.Clean(basePath) + string(filepath.Separator))
	candidate := strings.ToLower(filepath.Clean(candidatePath) + string(filepath.Separator))
	return strings.HasPrefix(candidate, base)
}
