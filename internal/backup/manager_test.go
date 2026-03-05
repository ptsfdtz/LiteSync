package backup

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"litesync/internal/api"
	"litesync/internal/logx"
)

func TestFullSyncCopiesAndExcludes(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	mustWriteFile(t, filepath.Join(src, "a.txt"), "hello")
	mustWriteFile(t, filepath.Join(src, "sub", "b.txt"), "world")
	mustWriteFile(t, filepath.Join(src, "sub", "c.tmp"), "temp")
	mustWriteFile(t, filepath.Join(src, ".git", "config"), "internal")

	job := api.Job{
		ID:        "job-1",
		Enabled:   true,
		SourceDir: src,
		TargetDir: dst,
		Exclude:   []string{".git/**", "*.tmp"},
		Strategy: api.Strategy{
			InitialSync:         "full",
			PreservePermissions: true,
		},
	}

	logger := logx.NewWithWriter("debug", io.Discard)
	m := New(logger)
	m.ReplaceJobs([]api.Job{job})

	result, err := m.SyncNow(context.Background(), api.SyncRequest{
		JobID:       job.ID,
		Reason:      api.TriggerStartup,
		Mode:        api.SyncModeFull,
		RequestedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if result.CopiedFiles != 2 {
		t.Fatalf("expected copied=2, got %d", result.CopiedFiles)
	}
	if result.ErrorCount != 0 {
		t.Fatalf("expected no errors, got %d", result.ErrorCount)
	}

	assertFileExists(t, filepath.Join(dst, "a.txt"))
	assertFileExists(t, filepath.Join(dst, "sub", "b.txt"))
	assertFileNotExists(t, filepath.Join(dst, "sub", "c.tmp"))
	assertFileNotExists(t, filepath.Join(dst, ".git", "config"))
}

func TestFullSyncUpdateAndSkip(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	mustWriteFile(t, filepath.Join(src, "a.txt"), "v1")

	job := api.Job{
		ID:        "job-2",
		Enabled:   true,
		SourceDir: src,
		TargetDir: dst,
		Strategy: api.Strategy{
			InitialSync:         "full",
			PreservePermissions: true,
		},
	}

	logger := logx.NewWithWriter("debug", io.Discard)
	m := New(logger)
	m.ReplaceJobs([]api.Job{job})

	_, err := m.SyncNow(context.Background(), api.SyncRequest{
		JobID:       job.ID,
		Reason:      api.TriggerStartup,
		Mode:        api.SyncModeFull,
		RequestedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("first sync failed: %v", err)
	}

	second, err := m.SyncNow(context.Background(), api.SyncRequest{
		JobID:       job.ID,
		Reason:      api.TriggerManual,
		Mode:        api.SyncModeFull,
		RequestedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("second sync failed: %v", err)
	}
	if second.CopiedFiles != 0 || second.UpdatedFiles != 0 {
		t.Fatalf("expected no copy/update, got copied=%d updated=%d", second.CopiedFiles, second.UpdatedFiles)
	}
	if second.SkippedFiles == 0 {
		t.Fatalf("expected skipped files > 0")
	}

	time.Sleep(1100 * time.Millisecond)
	mustWriteFile(t, filepath.Join(src, "a.txt"), "v2")

	third, err := m.SyncNow(context.Background(), api.SyncRequest{
		JobID:       job.ID,
		Reason:      api.TriggerManual,
		Mode:        api.SyncModeFull,
		RequestedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("third sync failed: %v", err)
	}
	if third.UpdatedFiles == 0 {
		t.Fatalf("expected updated files > 0")
	}
}

func TestIncrementalSyncApplyCreateWriteAndDeletePropagate(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	job := api.Job{
		ID:        "job-inc-1",
		Enabled:   true,
		SourceDir: src,
		TargetDir: dst,
		Strategy: api.Strategy{
			InitialSync:         "full",
			DeletePolicy:        "propagate",
			PreservePermissions: true,
		},
	}

	logger := logx.NewWithWriter("debug", io.Discard)
	m := New(logger)
	m.ReplaceJobs([]api.Job{job})

	mustWriteFile(t, filepath.Join(src, "old.txt"), "old")
	_, err := m.SyncNow(context.Background(), api.SyncRequest{
		JobID:       job.ID,
		Reason:      api.TriggerStartup,
		Mode:        api.SyncModeFull,
		RequestedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("baseline full sync failed: %v", err)
	}

	time.Sleep(1100 * time.Millisecond)
	mustWriteFile(t, filepath.Join(src, "old.txt"), "old-updated")
	mustWriteFile(t, filepath.Join(src, "new.txt"), "new")
	mustWriteFile(t, filepath.Join(dst, "to-delete.txt"), "delete-me")

	res, err := m.SyncByEvents(context.Background(), job.ID, []api.FileEvent{
		{JobID: job.ID, Path: filepath.Join(src, "new.txt"), Op: api.FileCreate, OccurredAt: time.Now()},
		{JobID: job.ID, Path: filepath.Join(src, "old.txt"), Op: api.FileWrite, OccurredAt: time.Now()},
		{JobID: job.ID, Path: filepath.Join(src, "to-delete.txt"), Op: api.FileRemove, OccurredAt: time.Now()},
	}, api.TriggerFileEvent)
	if err != nil {
		t.Fatalf("incremental sync failed: %v", err)
	}
	if res.CopiedFiles == 0 {
		t.Fatalf("expected copied files > 0")
	}
	if res.UpdatedFiles == 0 {
		t.Fatalf("expected updated files > 0")
	}
	if res.DeletedFiles == 0 {
		t.Fatalf("expected deleted files > 0")
	}

	assertFileExists(t, filepath.Join(dst, "new.txt"))
	assertFileExists(t, filepath.Join(dst, "old.txt"))
	assertFileNotExists(t, filepath.Join(dst, "to-delete.txt"))
}

func TestIncrementalSyncDeleteIgnore(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	job := api.Job{
		ID:        "job-inc-2",
		Enabled:   true,
		SourceDir: src,
		TargetDir: dst,
		Strategy: api.Strategy{
			InitialSync:         "full",
			DeletePolicy:        "ignore",
			PreservePermissions: true,
		},
	}

	logger := logx.NewWithWriter("debug", io.Discard)
	m := New(logger)
	m.ReplaceJobs([]api.Job{job})

	mustWriteFile(t, filepath.Join(src, "keep.txt"), "v1")
	_, err := m.SyncNow(context.Background(), api.SyncRequest{
		JobID:       job.ID,
		Reason:      api.TriggerStartup,
		Mode:        api.SyncModeFull,
		RequestedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("baseline full sync failed: %v", err)
	}

	if err := os.Remove(filepath.Join(src, "keep.txt")); err != nil {
		t.Fatalf("remove source file failed: %v", err)
	}

	res, err := m.SyncByEvents(context.Background(), job.ID, []api.FileEvent{
		{JobID: job.ID, Path: filepath.Join(src, "keep.txt"), Op: api.FileRemove, OccurredAt: time.Now()},
	}, api.TriggerFileEvent)
	if err != nil {
		t.Fatalf("incremental sync failed: %v", err)
	}
	if res.DeletedFiles != 0 {
		t.Fatalf("expected deleted=0, got %d", res.DeletedFiles)
	}
	assertFileExists(t, filepath.Join(dst, "keep.txt"))
}

func TestIncrementalSyncExcludeRule(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	job := api.Job{
		ID:        "job-inc-3",
		Enabled:   true,
		SourceDir: src,
		TargetDir: dst,
		Exclude:   []string{"*.tmp"},
		Strategy: api.Strategy{
			InitialSync:         "full",
			DeletePolicy:        "propagate",
			PreservePermissions: true,
		},
	}

	logger := logx.NewWithWriter("debug", io.Discard)
	m := New(logger)
	m.ReplaceJobs([]api.Job{job})

	mustWriteFile(t, filepath.Join(src, "ignore.tmp"), "tmp")
	res, err := m.SyncByEvents(context.Background(), job.ID, []api.FileEvent{
		{JobID: job.ID, Path: filepath.Join(src, "ignore.tmp"), Op: api.FileCreate, OccurredAt: time.Now()},
	}, api.TriggerFileEvent)
	if err != nil {
		t.Fatalf("incremental sync failed: %v", err)
	}
	if res.SkippedFiles == 0 {
		t.Fatalf("expected skipped files > 0")
	}
	assertFileNotExists(t, filepath.Join(dst, "ignore.tmp"))
}

func TestIncrementalSyncRetry(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	filePath := filepath.Join(src, "retry.txt")
	mustWriteFile(t, filePath, "retry")

	job := api.Job{
		ID:        "job-inc-4",
		Enabled:   true,
		SourceDir: src,
		TargetDir: dst,
		Strategy: api.Strategy{
			InitialSync:         "full",
			DeletePolicy:        "propagate",
			PreservePermissions: true,
		},
	}

	logger := logx.NewWithWriter("debug", io.Discard)
	m := New(logger)
	m.ReplaceJobs([]api.Job{job})

	var attempts int32
	realCopy := m.copyOp
	m.copyOp = func(srcPath, dstPath string, srcInfo os.FileInfo, preservePerm bool) (copyStatus, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			return fileSkipped, os.ErrPermission
		}
		return realCopy(srcPath, dstPath, srcInfo, preservePerm)
	}
	m.sleepFn = func(_ time.Duration) {}

	res, err := m.SyncByEvents(context.Background(), job.ID, []api.FileEvent{
		{JobID: job.ID, Path: filePath, Op: api.FileCreate, OccurredAt: time.Now()},
	}, api.TriggerFileEvent)
	if err != nil {
		t.Fatalf("incremental sync failed after retry: %v", err)
	}
	if atomic.LoadInt32(&attempts) < 3 {
		t.Fatalf("expected retries, got attempts=%d", attempts)
	}
	if res.CopiedFiles == 0 && res.UpdatedFiles == 0 {
		t.Fatalf("expected file copied/updated after retry")
	}
}

func TestIncrementalSyncJobNotFound(t *testing.T) {
	logger := logx.NewWithWriter("debug", io.Discard)
	m := New(logger)
	_, err := m.SyncByEvents(context.Background(), "missing", nil, api.TriggerFileEvent)
	if err == nil {
		t.Fatalf("expected error for missing job")
	}
	if !strings.Contains(err.Error(), "job not found") {
		t.Fatalf("expected job not found error, got %v", err)
	}
}

func TestRuntimeSnapshotUpdatedAfterSync(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	job := api.Job{
		ID:        "job-state",
		Enabled:   true,
		SourceDir: src,
		TargetDir: dst,
		Strategy: api.Strategy{
			InitialSync:         "full",
			DeletePolicy:        "propagate",
			PreservePermissions: true,
		},
	}

	mustWriteFile(t, filepath.Join(src, "a.txt"), "hello")
	logger := logx.NewWithWriter("debug", io.Discard)
	m := New(logger)
	m.ReplaceJobs([]api.Job{job})

	if _, err := m.SyncNow(context.Background(), api.SyncRequest{
		JobID:       job.ID,
		Reason:      api.TriggerStartup,
		Mode:        api.SyncModeFull,
		RequestedAt: time.Now(),
	}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	snapshot := m.RuntimeSnapshot()
	if len(snapshot.Jobs) == 0 {
		t.Fatalf("expected runtime states")
	}
	found := false
	for _, st := range snapshot.Jobs {
		if st.JobID == job.ID {
			found = true
			if st.LastErrorCode != "OK" {
				t.Fatalf("expected OK error code, got %s", st.LastErrorCode)
			}
			if st.LastRunID == "" {
				t.Fatalf("expected run id")
			}
		}
	}
	if !found {
		t.Fatalf("job state not found")
	}
}

func TestConflictPolicySkip(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	srcFile := filepath.Join(src, "a.txt")
	dstFile := filepath.Join(dst, "a.txt")
	mustWriteFile(t, srcFile, "source-v1")
	mustWriteFile(t, dstFile, "target-v2")
	time.Sleep(1100 * time.Millisecond)
	mustWriteFile(t, srcFile, "source-v3")

	job := api.Job{
		ID:        "job-conf-skip",
		Enabled:   true,
		SourceDir: src,
		TargetDir: dst,
		Strategy: api.Strategy{
			InitialSync:         "full",
			DeletePolicy:        "propagate",
			ConflictPolicy:      "skip",
			PreservePermissions: true,
		},
	}

	logger := logx.NewWithWriter("debug", io.Discard)
	m := New(logger)
	m.ReplaceJobs([]api.Job{job})

	res, err := m.SyncByEvents(context.Background(), job.ID, []api.FileEvent{
		{JobID: job.ID, Path: srcFile, Op: api.FileWrite, OccurredAt: time.Now()},
	}, api.TriggerFileEvent)
	if err != nil {
		t.Fatalf("sync by events failed: %v", err)
	}
	if res.ConflictCount == 0 {
		t.Fatalf("expected conflict count > 0")
	}

	got, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("read dst failed: %v", err)
	}
	if string(got) != "target-v2" {
		t.Fatalf("expected dst unchanged, got: %s", string(got))
	}
}

func TestConflictPolicyBackupThenOverwrite(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	srcFile := filepath.Join(src, "a.txt")
	dstFile := filepath.Join(dst, "a.txt")
	mustWriteFile(t, srcFile, "source-v1")
	mustWriteFile(t, dstFile, "target-v2")
	time.Sleep(1100 * time.Millisecond)
	mustWriteFile(t, srcFile, "source-v3")

	job := api.Job{
		ID:        "job-conf-backup",
		Enabled:   true,
		SourceDir: src,
		TargetDir: dst,
		Strategy: api.Strategy{
			InitialSync:         "full",
			DeletePolicy:        "propagate",
			ConflictPolicy:      "backup_then_overwrite",
			PreservePermissions: true,
		},
	}

	logger := logx.NewWithWriter("debug", io.Discard)
	m := New(logger)
	m.nowFn = func() time.Time { return time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC) }
	m.ReplaceJobs([]api.Job{job})

	res, err := m.SyncByEvents(context.Background(), job.ID, []api.FileEvent{
		{JobID: job.ID, Path: srcFile, Op: api.FileWrite, OccurredAt: time.Now()},
	}, api.TriggerFileEvent)
	if err != nil {
		t.Fatalf("sync by events failed: %v", err)
	}
	if res.ConflictCount == 0 {
		t.Fatalf("expected conflict count > 0")
	}

	got, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("read dst failed: %v", err)
	}
	if string(got) != "source-v3" {
		t.Fatalf("expected dst overwritten, got: %s", string(got))
	}

	backupFile := filepath.Join(dst, ".litesync_conflicts", "20260305-120000", "a.txt")
	backupData, err := os.ReadFile(backupFile)
	if err != nil {
		t.Fatalf("read backup file failed: %v", err)
	}
	if string(backupData) != "target-v2" {
		t.Fatalf("unexpected backup content: %s", string(backupData))
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file exists %s: %v", path, err)
	}
}

func assertFileNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file not exists %s, got err=%v", path, err)
	}
}
