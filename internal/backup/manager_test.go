package backup

import (
	"context"
	"io"
	"os"
	"path/filepath"
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
