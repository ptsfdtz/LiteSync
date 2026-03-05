package config

import (
	"context"
	"path/filepath"
	"testing"

	"litesync/internal/api"
)

func TestValidateRejectSameSourceAndTarget(t *testing.T) {
	svc := NewFileService(filepath.Join(t.TempDir(), "config.yaml"))
	base := t.TempDir()
	same := filepath.Join(base, "src")
	cfg := api.DefaultConfig()
	cfg.Jobs = append(cfg.Jobs, api.Job{
		ID:        "job-1",
		Enabled:   true,
		SourceDir: same,
		TargetDir: same,
		Strategy: api.Strategy{
			Mode:        "mirror",
			InitialSync: "full",
			EventSync: api.EventSync{
				DebounceMS: 1000,
			},
			PeriodicReconcile: api.PeriodicReconcile{
				Enabled:         true,
				IntervalMinutes: 30,
			},
			DeletePolicy:   "propagate",
			ConflictPolicy: "backup_then_overwrite",
		},
	})

	if err := svc.Validate(cfg); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	svc := NewFileService(path)

	cfg := api.DefaultConfig()
	cfg.Jobs = append(cfg.Jobs, api.Job{
		ID:        "job-1",
		Enabled:   true,
		SourceDir: filepath.Join(t.TempDir(), "src"),
		TargetDir: filepath.Join(t.TempDir(), "dst"),
		Strategy: api.Strategy{
			Mode:        "mirror",
			InitialSync: "full",
			EventSync: api.EventSync{
				DebounceMS: 1000,
			},
			PeriodicReconcile: api.PeriodicReconcile{
				Enabled:         true,
				IntervalMinutes: 30,
			},
			DeletePolicy:   "propagate",
			ConflictPolicy: "backup_then_overwrite",
		},
	})

	if err := svc.Save(context.Background(), cfg); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	got, err := svc.Load(context.Background())
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(got.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(got.Jobs))
	}
	if got.Jobs[0].ID != "job-1" {
		t.Fatalf("unexpected job id: %s", got.Jobs[0].ID)
	}
}
