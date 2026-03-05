package backup

import (
	"context"
	"time"

	"litesync/internal/api"
)

// Manager 是同步引擎占位实现，后续会在该模块实现全量/增量同步逻辑。
type Manager struct {
	logger api.Logger
}

func New(logger api.Logger) *Manager {
	return &Manager{logger: logger}
}

func (m *Manager) SyncNow(_ context.Context, req api.SyncRequest) (api.SyncResult, error) {
	result := api.SyncResult{
		JobID:      req.JobID,
		RunID:      api.RunID(""),
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
	}
	m.logger.Warn("backup sync is not implemented", api.Field{Key: "job_id", Value: req.JobID})
	return result, api.ErrNotImplemented
}

func (m *Manager) SyncByEvents(_ context.Context, jobID api.JobID, _ []api.FileEvent, _ api.TriggerReason) (api.SyncResult, error) {
	result := api.SyncResult{
		JobID:      jobID,
		RunID:      api.RunID(""),
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
	}
	m.logger.Warn("incremental sync is not implemented", api.Field{Key: "job_id", Value: jobID})
	return result, api.ErrNotImplemented
}

func (m *Manager) Reconcile(_ context.Context, jobID api.JobID) (api.SyncResult, error) {
	result := api.SyncResult{
		JobID:      jobID,
		RunID:      api.RunID(""),
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
	}
	m.logger.Warn("reconcile is not implemented", api.Field{Key: "job_id", Value: jobID})
	return result, api.ErrNotImplemented
}

func (m *Manager) Cancel(_ context.Context, _ api.RunID) error {
	return nil
}
