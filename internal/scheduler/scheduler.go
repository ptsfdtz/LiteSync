package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"litesync/internal/api"
)

// Dispatcher 是调度模块占位实现，仅提供任务注册与手动触发骨架。
type Dispatcher struct {
	mu      sync.RWMutex
	started bool
	jobs    map[api.JobID]struct{}
	backup  api.BackupManager
	logger  api.Logger
}

func New(backup api.BackupManager, logger api.Logger) *Dispatcher {
	return &Dispatcher{
		jobs:   make(map[api.JobID]struct{}),
		backup: backup,
		logger: logger,
	}
}

func (d *Dispatcher) RegisterJob(_ context.Context, jobID api.JobID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.jobs[jobID] = struct{}{}
	return nil
}

func (d *Dispatcher) UnregisterJob(_ context.Context, jobID api.JobID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.jobs, jobID)
	return nil
}

func (d *Dispatcher) PushEvent(_ context.Context, event api.FileEvent) error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if !d.started {
		return api.Wrap(api.ErrInternal, "scheduler not started")
	}
	if _, ok := d.jobs[event.JobID]; !ok {
		return api.Wrap(api.ErrJobNotFound, fmt.Sprintf("job_id=%s", event.JobID))
	}
	d.logger.Debug("event accepted by scheduler", api.Field{Key: "job_id", Value: event.JobID}, api.Field{Key: "path", Value: event.Path})
	return nil
}

func (d *Dispatcher) TriggerNow(ctx context.Context, jobID api.JobID, reason api.TriggerReason) (api.RunID, error) {
	d.mu.RLock()
	started := d.started
	_, exists := d.jobs[jobID]
	d.mu.RUnlock()

	if !started {
		return "", api.Wrap(api.ErrInternal, "scheduler not started")
	}
	if !exists {
		return "", api.Wrap(api.ErrJobNotFound, fmt.Sprintf("job_id=%s", jobID))
	}

	runID := api.RunID(fmt.Sprintf("run-%d", time.Now().UnixNano()))
	_, err := d.backup.SyncNow(ctx, api.SyncRequest{
		JobID:       jobID,
		RequestID:   api.RequestID(runID),
		Reason:      reason,
		Mode:        api.SyncModeFull,
		RequestedAt: time.Now(),
	})
	return runID, err
}

func (d *Dispatcher) Start(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.started = true
	return nil
}

func (d *Dispatcher) Stop(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.started = false
	return nil
}
