package state

import (
	"path/filepath"
	"testing"
	"time"

	"litesync/internal/api"
)

func TestSaveLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store := NewFileStore(dir)

	in := api.RuntimeSnapshot{
		GeneratedAt: time.Now(),
		Jobs: []api.JobRuntimeState{
			{
				JobID:         "job-1",
				LastRunID:     "run-1",
				LastReason:    "startup",
				LastErrorCode: "OK",
			},
		},
	}
	if err := store.Save(in); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	out, err := store.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(out.Jobs) != 1 {
		t.Fatalf("expected one job state, got %d", len(out.Jobs))
	}
	if out.Jobs[0].JobID != "job-1" {
		t.Fatalf("unexpected job id: %s", out.Jobs[0].JobID)
	}
}
