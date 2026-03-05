package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"litesync/internal/api"
)

type ReportExporter struct {
	dir string
}

func NewReportExporter(stateDir string) *ReportExporter {
	return &ReportExporter{dir: filepath.Clean(filepath.Join(stateDir, "reports"))}
}

func (e *ReportExporter) Export(summary api.RuntimeSummary, snapshot api.RuntimeSnapshot) (string, error) {
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		return "", api.Wrap(api.ErrPermissionDenied, "create report dir failed")
	}

	payload := struct {
		ExportedAt time.Time           `json:"exported_at"`
		Summary    api.RuntimeSummary  `json:"summary"`
		Detail     api.RuntimeSnapshot `json:"detail"`
	}{
		ExportedAt: time.Now(),
		Summary:    summary,
		Detail:     snapshot,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", api.Wrap(api.ErrInternal, "encode report failed")
	}

	path := filepath.Join(e.dir, "sync-report-"+time.Now().Format("20060102-150405")+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", api.Wrap(api.ErrIOTransient, "write report failed")
	}
	return path, nil
}
