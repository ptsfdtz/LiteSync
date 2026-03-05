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
	"runtime"
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
	state  map[api.JobID]api.JobRuntimeState

	copyOp   func(srcPath, dstPath string, srcInfo fs.FileInfo, preservePerm bool) (copyStatus, error)
	removeOp func(targetPath string) (bool, error)
	sleepFn  func(d time.Duration)
	nowFn    func() time.Time
}

func New(logger api.Logger) *Manager {
	return &Manager{
		logger: logger,
		jobs:   make(map[api.JobID]api.Job),
		state:  make(map[api.JobID]api.JobRuntimeState),
		copyOp: copyFileWithMode,
		removeOp: func(targetPath string) (bool, error) {
			return removePath(targetPath)
		},
		sleepFn: time.Sleep,
		nowFn:   time.Now,
	}
}

func (m *Manager) ReplaceJobs(jobs []api.Job) {
	m.mu.Lock()
	defer m.mu.Unlock()

	next := make(map[api.JobID]api.Job, len(jobs))
	for _, job := range jobs {
		next[job.ID] = job
		if _, ok := m.state[job.ID]; !ok {
			m.state[job.ID] = api.JobRuntimeState{JobID: job.ID}
		}
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
		m.recordState(req.JobID, req.Reason, result, err)
		runLogger.Error("full sync finished with errors", err, summaryFields(result)...)
		return result, err
	}

	m.recordState(req.JobID, req.Reason, result, nil)
	runLogger.Info("full sync completed", summaryFields(result)...)
	return result, nil
}

func (m *Manager) SyncByEvents(ctx context.Context, jobID api.JobID, events []api.FileEvent, reason api.TriggerReason) (api.SyncResult, error) {
	job, ok := m.lookupJob(jobID)
	if !ok {
		return api.SyncResult{}, api.Wrap(api.ErrJobNotFound, fmt.Sprintf("job_id=%s", jobID))
	}

	runID := api.RunID(fmt.Sprintf("run-%d", time.Now().UnixNano()))
	runLogger := m.logger.With(
		api.Field{Key: "job_id", Value: jobID},
		api.Field{Key: "run_id", Value: runID},
	)

	result := api.SyncResult{
		JobID:     jobID,
		RunID:     runID,
		StartedAt: time.Now(),
	}

	normalized := compactEvents(events)
	runLogger.Info(
		"incremental sync started",
		api.Field{Key: "reason", Value: reason},
		api.Field{Key: "events_in", Value: len(events)},
		api.Field{Key: "events_normalized", Value: len(normalized)},
	)

	matcher := newExcludeMatcher(job.Exclude)
	var failed []error
	for _, event := range normalized {
		select {
		case <-ctx.Done():
			result.FinishedAt = time.Now()
			return result, ctx.Err()
		default:
		}

		if err := m.handleIncrementalEvent(ctx, job, matcher, event, &result, runLogger); err != nil {
			result.ErrorCount++
			failed = append(failed, err)
		}
	}

	result.FinishedAt = time.Now()
	if len(failed) > 0 {
		err := api.Wrap(api.ErrIOTransient, fmt.Sprintf("%d incremental operations failed", len(failed)))
		m.recordState(jobID, reason, result, err)
		runLogger.Error("incremental sync finished with errors", err, summaryFields(result)...)
		return result, err
	}

	m.recordState(jobID, reason, result, nil)
	runLogger.Info("incremental sync completed", summaryFields(result)...)
	return result, nil
}

func (m *Manager) Reconcile(ctx context.Context, jobID api.JobID) (api.SyncResult, error) {
	req := api.SyncRequest{
		JobID:       jobID,
		RequestID:   api.RequestID(fmt.Sprintf("reconcile-%d", time.Now().UnixNano())),
		Reason:      api.TriggerReconcile,
		Mode:        api.SyncModeFull,
		RequestedAt: time.Now(),
	}
	return m.SyncNow(ctx, req)
}

func (m *Manager) Cancel(_ context.Context, _ api.RunID) error {
	return nil
}

func (m *Manager) RuntimeSnapshot() api.RuntimeSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := api.RuntimeSnapshot{
		GeneratedAt: time.Now(),
		Jobs:        make([]api.JobRuntimeState, 0, len(m.state)),
	}
	for _, st := range m.state {
		snapshot.Jobs = append(snapshot.Jobs, st)
	}
	return snapshot
}

func (m *Manager) handleIncrementalEvent(
	ctx context.Context,
	job api.Job,
	matcher excludeMatcher,
	event api.FileEvent,
	result *api.SyncResult,
	logger api.Logger,
) error {
	srcPath := filepath.Clean(event.Path)
	if srcPath == "" {
		result.SkippedFiles++
		return nil
	}

	relPath, targetPath, err := resolveTargetPath(job, srcPath)
	if err != nil {
		logger.Warn("skip event outside source root", api.Field{Key: "path", Value: srcPath})
		result.SkippedFiles++
		return nil
	}

	if matcher.Match(relPath, event.IsDir) {
		result.SkippedFiles++
		return nil
	}

	switch event.Op {
	case api.FileRemove, api.FileRename:
		return m.applyDelete(ctx, job, targetPath, relPath, result, logger)
	case api.FileCreate, api.FileWrite, api.FileChmod:
		return m.applyUpsert(ctx, job, srcPath, targetPath, relPath, result, logger)
	default:
		result.SkippedFiles++
		return nil
	}
}

func (m *Manager) recordState(jobID api.JobID, reason api.TriggerReason, result api.SyncResult, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state[jobID] = api.JobRuntimeState{
		JobID:         jobID,
		LastRunID:     result.RunID,
		LastReason:    string(reason),
		LastErrorCode: api.ErrorCode(err),
		StartedAt:     result.StartedAt,
		FinishedAt:    result.FinishedAt,
		CopiedFiles:   result.CopiedFiles,
		UpdatedFiles:  result.UpdatedFiles,
		DeletedFiles:  result.DeletedFiles,
		SkippedFiles:  result.SkippedFiles,
		ConflictCount: result.ConflictCount,
		ErrorCount:    result.ErrorCount,
	}
}

func (m *Manager) applyDelete(
	ctx context.Context,
	job api.Job,
	targetPath string,
	relPath string,
	result *api.SyncResult,
	logger api.Logger,
) error {
	deletePolicy := strings.ToLower(strings.TrimSpace(job.Strategy.DeletePolicy))
	switch deletePolicy {
	case "", api.DefaultDeletePolicy:
		var removed bool
		err := m.withRetry(ctx, 3, 120*time.Millisecond, func() error {
			var opErr error
			removed, opErr = m.removeOp(targetPath)
			return opErr
		})
		if err != nil {
			logger.Warn("delete target failed", api.Field{Key: "path", Value: relPath}, api.Field{Key: "error", Value: err.Error()})
			return err
		}
		if removed {
			result.DeletedFiles++
		} else {
			result.SkippedFiles++
		}
		return nil
	case "ignore", "soft_delete":
		result.SkippedFiles++
		return nil
	default:
		result.SkippedFiles++
		return api.Wrap(api.ErrInvalidArgument, fmt.Sprintf("unsupported delete_policy=%s", deletePolicy))
	}
}

func (m *Manager) applyUpsert(
	ctx context.Context,
	job api.Job,
	srcPath string,
	targetPath string,
	relPath string,
	result *api.SyncResult,
	logger api.Logger,
) error {
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// 文件事件可能晚于文件生命周期，按可恢复语义跳过即可。
			result.SkippedFiles++
			return nil
		}
		logger.Warn("read source info failed", api.Field{Key: "path", Value: relPath}, api.Field{Key: "error", Value: err.Error()})
		return err
	}

	if srcInfo.IsDir() {
		if err := os.MkdirAll(targetPath, 0o755); err != nil {
			logger.Warn("create target dir failed", api.Field{Key: "path", Value: relPath}, api.Field{Key: "error", Value: err.Error()})
			return err
		}
		result.SkippedFiles++
		return nil
	}

	var status copyStatus
	err = m.withRetry(ctx, 3, 120*time.Millisecond, func() error {
		proceed, conflictErr := m.handleConflict(job, srcInfo, targetPath, relPath, result)
		if conflictErr != nil {
			return conflictErr
		}
		if !proceed {
			status = fileSkipped
			return nil
		}

		var opErr error
		status, opErr = m.copyOp(srcPath, targetPath, srcInfo, job.Strategy.PreservePermissions)
		return opErr
	})
	if err != nil {
		logger.Warn("copy target failed", api.Field{Key: "path", Value: relPath}, api.Field{Key: "error", Value: err.Error()})
		return err
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
}

func (m *Manager) withRetry(ctx context.Context, maxAttempts int, baseBackoff time.Duration, fn func() error) error {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if !shouldRetry(err) || attempt == maxAttempts {
			break
		}

		backoff := baseBackoff * time.Duration(1<<(attempt-1))
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		m.sleepFn(backoff)
	}
	return lastErr
}

func shouldRetry(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, os.ErrNotExist)
}

func resolveTargetPath(job api.Job, sourcePath string) (string, string, error) {
	rel, err := filepath.Rel(job.SourceDir, sourcePath)
	if err != nil {
		return "", "", err
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == string(filepath.Separator) {
		return "", "", errors.New("root path")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", errors.New("path outside source")
	}
	rel = filepath.ToSlash(rel)
	targetPath := filepath.Join(job.TargetDir, filepath.FromSlash(rel))
	return rel, targetPath, nil
}

func (m *Manager) handleConflict(
	job api.Job,
	srcInfo fs.FileInfo,
	targetPath string,
	relPath string,
	result *api.SyncResult,
) (bool, error) {
	dstInfo, err := os.Stat(targetPath)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if sameFileState(srcInfo, dstInfo) {
		return true, nil
	}

	policy := strings.ToLower(strings.TrimSpace(job.Strategy.ConflictPolicy))
	if policy == "" {
		policy = api.DefaultConflictPolicy
	}

	switch policy {
	case "overwrite":
		result.ConflictCount++
		return true, nil
	case "backup_then_overwrite":
		result.ConflictCount++
		if err := m.backupConflict(targetPath, relPath, job.TargetDir); err != nil {
			return false, err
		}
		return true, nil
	case "skip":
		result.ConflictCount++
		result.SkippedFiles++
		return false, nil
	default:
		return false, api.Wrap(api.ErrInvalidArgument, fmt.Sprintf("unsupported conflict_policy=%s", policy))
	}
}

func (m *Manager) backupConflict(targetPath, relPath, targetRoot string) error {
	ts := m.nowFn().Format("20060102-150405")
	backupPath := filepath.Join(targetRoot, ".litesync_conflicts", ts, filepath.FromSlash(relPath))

	info, err := os.Stat(targetPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(targetPath, backupPath)
	}

	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return err
	}

	src, err := os.Open(targetPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return nil
}

func copyDir(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		_, err = copyFileWithMode(path, target, info, true)
		return err
	})
}

func compactEvents(events []api.FileEvent) []api.FileEvent {
	latest := make(map[string]api.FileEvent, len(events))
	order := make([]string, 0, len(events))
	for _, event := range events {
		path := filepath.Clean(event.Path)
		if path == "" || path == "." {
			continue
		}
		key := normalizeEventKey(path)
		if _, ok := latest[key]; !ok {
			order = append(order, key)
		}
		event.Path = path
		latest[key] = event
	}

	out := make([]api.FileEvent, 0, len(order))
	for _, key := range order {
		out = append(out, latest[key])
	}
	return out
}

func normalizeEventKey(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
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

		proceed, err := m.handleConflict(job, info, dstPath, relPath, result)
		if err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", relPath, err))
			result.ErrorCount++
			logger.Warn("conflict handling failed", api.Field{Key: "path", Value: relPath}, api.Field{Key: "error", Value: err.Error()})
			return nil
		}
		if !proceed {
			return nil
		}

		status, err := m.copyOp(srcPath, dstPath, info, job.Strategy.PreservePermissions)
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

func removePath(targetPath string) (bool, error) {
	_, err := os.Stat(targetPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := os.RemoveAll(targetPath); err != nil {
		return false, err
	}
	return true, nil
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
