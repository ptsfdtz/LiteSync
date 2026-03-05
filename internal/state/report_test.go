package state

import (
	"path/filepath"
	"testing"
	"time"

	"litesync/internal/api"
)

func TestExportReport(t *testing.T) {
	exporter := NewReportExporter(filepath.Join(t.TempDir(), "state"))
	summary := api.RuntimeSummary{
		GeneratedAt: time.Now(),
		JobCount:    1,
		ErrorCodes:  map[string]uint64{"OK": 1},
	}
	snapshot := api.RuntimeSnapshot{
		GeneratedAt: time.Now(),
		Jobs: []api.JobRuntimeState{
			{JobID: "job-1", LastErrorCode: "OK"},
		},
	}

	path, err := exporter.Export(summary, snapshot)
	if err != nil {
		t.Fatalf("export report failed: %v", err)
	}
	if path == "" {
		t.Fatalf("expected non-empty report path")
	}
}
