package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"litesync/internal/api"
)

// Manager 是同步引擎实现，当前已支持首次全量同步。
type Manager struct {
	logger api.Logger
	mu     sync.RWMutex
	jobs   map[api.JobID]api.Job
}

func New(logger api.Logger) *Manager {
	return &Manager{
		logger: logger,
		jobs:   make(map[api.JobID]api.Job),
	}
}

func (m *Manager) ReplaceJobs(jobs []api.Job) {
	m.mu.Lock()
	defer m.mu.Unlock()

	next := make(map[api.JobID]api.Job, len(jobs))
	for _, job := range jobs {
		next[job.ID] = job
	}
	m.jobs = next
}

func (m *Manager) SyncNow(ctx context.Context, req api.SyncRequest) (api.SyncResult, error) {
	job, ok := m.lookupJob(req.JobID)
	if !ok {
		return api.SyncResult{}, api.Wrap(api.ErrJobNotFound, fmt.Sprintf("job_id=%s", req.JobID))
	}

	if req.Mode != api.SyncModeFull {
		return api.SyncResult{}, api.Wrap(api.ErrNotImplemented, "only full sync is implemented")
	}

	runID := api.RunID(fmt.Sprintf("run-%d", time.Now().UnixNano()))
	runLogger := m.logger.With(
		api.Field{Key: "job_id", Value: req.JobID},
		api.Field{Key: "run_id", Value: runID},
	)

	startedAt := time.Now()
	result := api.SyncResult{
		JobID:     req.JobID,
		RunID:     runID,
		StartedAt: startedAt,
	}

	runLogger.Info(
		"full sync started",
		api.Field{Key: "source_dir", Value: job.SourceDir},
		api.Field{Key: "target_dir", Value: job.TargetDir},
		api.Field{Key: "exclude_rules", Value: len(job.Exclude)},
	)

	err := m.syncFull(ctx, job, &result, runLogger)
	result.FinishedAt = time.Now()
	if err != nil {
		runLogger.Error("full sync finished with errors", err, summaryFields(result)...)
		return result, err
	}

	runLogger.Info("full sync completed", summaryFields(result)...)
	return result, nil
}

func (m *Manager) SyncByEvents(_ context.Context, jobID api.JobID, _ []api.FileEvent, _ api.TriggerReason) (api.SyncResult, error) {
	result := api.SyncResult{
		JobID:      jobID,
		RunID:      api.RunID(""),
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
	}
	m.logger.Warn("incremental sync is not implemented", api.Field{Key: "job_id", Value: jobID})
	return result, api.ErrNotImplemented
}

func (m *Manager) Reconcile(_ context.Context, jobID api.JobID) (api.SyncResult, error) {
	result := api.SyncResult{
		JobID:      jobID,
		RunID:      api.RunID(""),
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
	}
	m.logger.Warn("reconcile is not implemented", api.Field{Key: "job_id", Value: jobID})
	return result, api.ErrNotImplemented
}

func (m *Manager) Cancel(_ context.Context, _ api.RunID) error {
	return nil
}

func (m *Manager) lookupJob(id api.JobID) (api.Job, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	return job, ok
}

func (m *Manager) syncFull(ctx context.Context, job api.Job, result *api.SyncResult, logger api.Logger) error {
	matcher := newExcludeMatcher(job.Exclude)
	var failed []error

	walkErr := filepath.WalkDir(job.SourceDir, func(srcPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			failed = append(failed, walkErr)
			result.ErrorCount++
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if srcPath == job.SourceDir {
			return nil
		}

		relPath, err := filepath.Rel(job.SourceDir, srcPath)
		if err != nil {
			failed = append(failed, err)
			result.ErrorCount++
			return nil
		}
		relPath = filepath.ToSlash(filepath.Clean(relPath))

		if matcher.Match(relPath, d.IsDir()) {
			result.SkippedFiles++
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		dstPath := filepath.Join(job.TargetDir, relPath)
		if d.IsDir() {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				failed = append(failed, fmt.Errorf("%s: %w", relPath, err))
				result.ErrorCount++
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", relPath, err))
			result.ErrorCount++
			return nil
		}

		status, err := copyFileWithMode(srcPath, dstPath, info, job.Strategy.PreservePermissions)
		if err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", relPath, err))
			result.ErrorCount++
			logger.Warn("copy file failed", api.Field{Key: "path", Value: relPath}, api.Field{Key: "error", Value: err.Error()})
			return nil
		}
		switch status {
		case fileCopied:
			result.CopiedFiles++
		case fileUpdated:
			result.UpdatedFiles++
		default:
			result.SkippedFiles++
		}
		return nil
	})

	if walkErr != nil {
		if errors.Is(walkErr, context.Canceled) {
			return walkErr
		}
		return api.Wrap(api.ErrIOTransient, walkErr.Error())
	}

	if len(failed) > 0 {
		return api.Wrap(api.ErrIOTransient, fmt.Sprintf("%d file operations failed", len(failed)))
	}
	return nil
}

type copyStatus int

const (
	fileSkipped copyStatus = iota
	fileCopied
	fileUpdated
)

func copyFileWithMode(srcPath, dstPath string, srcInfo fs.FileInfo, preservePerm bool) (copyStatus, error) {
	existed := false
	dstInfo, err := os.Stat(dstPath)
	if err == nil {
		existed = true
		if sameFileState(srcInfo, dstInfo) {
			return fileSkipped, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fileSkipped, err
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return fileSkipped, err
	}

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fileSkipped, err
	}
	defer srcFile.Close()

	tmpFile, err := os.CreateTemp(filepath.Dir(dstPath), ".litesync-*")
	if err != nil {
		return fileSkipped, err
	}
	tmpName := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmpFile, srcFile); err != nil {
		_ = tmpFile.Close()
		return fileSkipped, err
	}
	if err := tmpFile.Close(); err != nil {
		return fileSkipped, err
	}

	mode := fs.FileMode(0o644)
	if preservePerm {
		mode = srcInfo.Mode()
	}
	if err := os.Chmod(tmpName, mode.Perm()); err != nil {
		return fileSkipped, err
	}
	if err := os.Chtimes(tmpName, srcInfo.ModTime(), srcInfo.ModTime()); err != nil {
		return fileSkipped, err
	}
	if err := os.Rename(tmpName, dstPath); err != nil {
		return fileSkipped, err
	}
	cleanup = false

	if existed {
		return fileUpdated, nil
	}
	return fileCopied, nil
}

func sameFileState(src fs.FileInfo, dst fs.FileInfo) bool {
	if src.Size() != dst.Size() {
		return false
	}
	srcMod := src.ModTime().Truncate(time.Second)
	dstMod := dst.ModTime().Truncate(time.Second)
	return srcMod.Equal(dstMod)
}

type excludeMatcher struct {
	patterns []string
}

func newExcludeMatcher(patterns []string) excludeMatcher {
	cleaned := make([]string, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(filepath.ToSlash(p))
		if p == "" {
			continue
		}
		cleaned = append(cleaned, p)
	}
	return excludeMatcher{patterns: cleaned}
}

func (m excludeMatcher) Match(relPath string, isDir bool) bool {
	relPath = filepath.ToSlash(relPath)
	base := path.Base(relPath)
	for _, p := range m.patterns {
		if strings.HasSuffix(p, "/**") {
			prefix := strings.TrimSuffix(p, "/**")
			if prefix == "" {
				return true
			}
			if relPath == prefix || strings.HasPrefix(relPath, prefix+"/") {
				return true
			}
		}

		if ok, _ := path.Match(p, relPath); ok {
			return true
		}
		if ok, _ := path.Match(p, base); ok {
			return true
		}
		if isDir && (p == relPath || p == relPath+"/") {
			return true
		}
	}
	return false
}

func summaryFields(result api.SyncResult) []api.Field {
	return []api.Field{
		{Key: "copied", Value: result.CopiedFiles},
		{Key: "updated", Value: result.UpdatedFiles},
		{Key: "deleted", Value: result.DeletedFiles},
		{Key: "skipped", Value: result.SkippedFiles},
		{Key: "errors", Value: result.ErrorCount},
	}
}
