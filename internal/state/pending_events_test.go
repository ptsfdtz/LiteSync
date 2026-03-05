package state

import (
	"path/filepath"
	"testing"
	"time"

	"litesync/internal/api"
)

func TestPendingEventStoreRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store := NewPendingEventStore(dir)

	event := api.FileEvent{
		JobID:      "job-1",
		Path:       "/tmp/a.txt",
		Op:         api.FileWrite,
		OccurredAt: time.Now(),
	}
	if err := store.Add(event); err != nil {
		t.Fatalf("add event failed: %v", err)
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("load all failed: %v", err)
	}
	if len(all["job-1"]) != 1 {
		t.Fatalf("expected one event, got %d", len(all["job-1"]))
	}

	if err := store.Clear("job-1"); err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	all, err = store.LoadAll()
	if err != nil {
		t.Fatalf("load all failed: %v", err)
	}
	if len(all["job-1"]) != 0 {
		t.Fatalf("expected events cleared")
	}
}
