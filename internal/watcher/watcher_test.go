package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"litesync/internal/api"
)

func TestStartStopIdempotent(t *testing.T) {
	w := New()
	root := t.TempDir()
	jobID := api.JobID("job-1")

	if err := w.Start(context.Background(), jobID, root); err != nil {
		t.Fatalf("first start failed: %v", err)
	}
	if err := w.Start(context.Background(), jobID, root); err != nil {
		t.Fatalf("second start should be idempotent: %v", err)
	}
	if err := w.Stop(context.Background(), jobID); err != nil {
		t.Fatalf("first stop failed: %v", err)
	}
	if err := w.Stop(context.Background(), jobID); err != nil {
		t.Fatalf("second stop should be idempotent: %v", err)
	}
}

func TestMapFsnotifyOps(t *testing.T) {
	cases := []struct {
		name string
		in   fsnotify.Op
		want []api.FileOp
	}{
		{
			name: "create",
			in:   fsnotify.Create,
			want: []api.FileOp{api.FileCreate},
		},
		{
			name: "write+chmod",
			in:   fsnotify.Write | fsnotify.Chmod,
			want: []api.FileOp{api.FileWrite, api.FileChmod},
		},
		{
			name: "remove+rename",
			in:   fsnotify.Remove | fsnotify.Rename,
			want: []api.FileOp{api.FileRemove, api.FileRename},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := mapFsnotifyOps(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got=%v want=%v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("ops mismatch at %d: got=%v want=%v", i, got, tc.want)
				}
			}
		})
	}
}

func TestWatcherEmitsEventForCreatedFile(t *testing.T) {
	w := New()
	root := t.TempDir()
	jobID := api.JobID("job-evt")

	if err := w.Start(context.Background(), jobID, root); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer func() {
		_ = w.Stop(context.Background(), jobID)
	}()

	nestedDir := filepath.Join(root, "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	target := filepath.Join(nestedDir, "a.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	if !waitForEvent(t, w.Events(), w.Errors(), jobID, target, 5*time.Second) {
		t.Fatalf("did not receive event for file %s", target)
	}
}

func waitForEvent(t *testing.T, events <-chan api.FileEvent, errs <-chan error, jobID api.JobID, path string, timeout time.Duration) bool {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	target := filepath.Clean(path)
	for {
		select {
		case <-timer.C:
			return false
		case err := <-errs:
			t.Logf("watcher error: %v", err)
		case e := <-events:
			if e.JobID != jobID {
				continue
			}
			if filepath.Clean(e.Path) != target {
				continue
			}
			if e.Op == api.FileCreate || e.Op == api.FileWrite {
				return true
			}
		}
	}
}
