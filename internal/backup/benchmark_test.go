package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"litesync/internal/api"
	"litesync/internal/logx"
)

func BenchmarkFullSyncParallelCopies(b *testing.B) {
	for _, workers := range []int{1, 2, 4, 8} {
		workers := workers
		b.Run(fmt.Sprintf("workers_%d", workers), func(b *testing.B) {
			src := b.TempDir()
			dst := b.TempDir()

			for i := 0; i < 500; i++ {
				path := filepath.Join(src, fmt.Sprintf("file-%04d.txt", i))
				if err := os.WriteFile(path, []byte("benchmark-data"), 0o644); err != nil {
					b.Fatalf("write source file failed: %v", err)
				}
			}

			job := api.Job{
				ID:        "job-bench",
				Enabled:   true,
				SourceDir: src,
				TargetDir: dst,
				Strategy: api.Strategy{
					InitialSync:         "full",
					MaxParallelCopies:   workers,
					PreservePermissions: true,
				},
			}

			logger := logx.NewWithWriter("error", io.Discard)
			m := New(logger)
			m.ReplaceJobs([]api.Job{job})

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := m.SyncNow(context.Background(), api.SyncRequest{
					JobID:       job.ID,
					Reason:      api.TriggerManual,
					Mode:        api.SyncModeFull,
					RequestedAt: time.Now(),
				}); err != nil {
					b.Fatalf("sync now failed: %v", err)
				}
			}
		})
	}
}
