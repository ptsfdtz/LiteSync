package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"litesync/internal/api"
)

type Dispatcher struct {
	mu      sync.RWMutex
	started bool
	rootCtx context.Context
	cancel  context.CancelFunc

	jobs    map[api.JobID]*jobState
	configs map[api.JobID]jobConfig

	backup api.BackupManager
	logger api.Logger

	pendingStore PendingStore
}

type PendingStore interface {
	LoadAll() (map[api.JobID][]api.FileEvent, error)
	Add(event api.FileEvent) error
	Set(jobID api.JobID, events []api.FileEvent) error
	Clear(jobID api.JobID) error
}

type jobConfig struct {
	debounce          time.Duration
	reconcileEnabled  bool
	reconcileInterval time.Duration
}

type jobState struct {
	id               api.JobID
	debounce         time.Duration
	reconcileEnabled bool
	reconcileEvery   time.Duration

	eventCh  chan api.FileEvent
	manualCh chan manualRequest
	doneCh   chan struct{}

	cancel  context.CancelFunc
	running bool
}

type manualRequest struct {
	ctx      context.Context
	reason   api.TriggerReason
	response chan triggerResult
}

type triggerResult struct {
	runID api.RunID
	err   error
}

func New(backup api.BackupManager, logger api.Logger) *Dispatcher {
	return &Dispatcher{
		jobs:    make(map[api.JobID]*jobState),
		configs: make(map[api.JobID]jobConfig),
		backup:  backup,
		logger:  logger,
	}
}

func (d *Dispatcher) EnableRecovery(store PendingStore) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pendingStore = store
}

// ConfigureJobs 读取任务策略并预置调度参数。
// 该方法可在 Start 前或后调用；已运行任务会在下一次注册时应用新配置。
func (d *Dispatcher) ConfigureJobs(jobs []api.Job) {
	d.mu.Lock()
	defer d.mu.Unlock()

	next := make(map[api.JobID]jobConfig, len(jobs))
	for _, job := range jobs {
		debounce := time.Duration(job.Strategy.EventSync.DebounceMS) * time.Millisecond
		if debounce <= 0 {
			debounce = time.Duration(api.DefaultEventDebounceMS) * time.Millisecond
		}

		reconcileEvery := time.Duration(job.Strategy.PeriodicReconcile.IntervalMinutes) * time.Minute
		if reconcileEvery <= 0 {
			reconcileEvery = time.Duration(api.DefaultPeriodicIntervalMinutes) * time.Minute
		}
		next[job.ID] = jobConfig{
			debounce:          debounce,
			reconcileEnabled:  job.Strategy.PeriodicReconcile.Enabled,
			reconcileInterval: reconcileEvery,
		}
	}
	d.configs = next
}

func (d *Dispatcher) RegisterJob(_ context.Context, jobID api.JobID) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.jobs[jobID]; exists {
		return nil
	}

	state := &jobState{
		id:               jobID,
		debounce:         d.debounceFor(jobID),
		reconcileEnabled: d.reconcileEnabledFor(jobID),
		reconcileEvery:   d.reconcileEveryFor(jobID),
		eventCh:          make(chan api.FileEvent, 1024),
		manualCh:         make(chan manualRequest, 16),
		doneCh:           make(chan struct{}),
	}
	d.jobs[jobID] = state

	if d.started {
		d.startJobLocked(state)
	}
	return nil
}

func (d *Dispatcher) UnregisterJob(ctx context.Context, jobID api.JobID) error {
	d.mu.Lock()
	state, exists := d.jobs[jobID]
	if !exists {
		d.mu.Unlock()
		return nil
	}
	delete(d.jobs, jobID)
	d.mu.Unlock()

	d.stopJob(ctx, state)
	return nil
}

func (d *Dispatcher) PushEvent(ctx context.Context, event api.FileEvent) error {
	d.mu.RLock()
	started := d.started
	state, exists := d.jobs[event.JobID]
	d.mu.RUnlock()

	if !started {
		return api.Wrap(api.ErrInternal, "scheduler not started")
	}
	if !exists {
		return api.Wrap(api.ErrJobNotFound, fmt.Sprintf("job_id=%s", event.JobID))
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case state.eventCh <- event:
		if d.pendingStore != nil {
			if err := d.pendingStore.Add(event); err != nil {
				d.logger.Warn("persist pending event failed", api.Field{Key: "job_id", Value: event.JobID}, api.Field{Key: "error", Value: err.Error()})
			}
		}
		return nil
	default:
		return api.Wrap(api.ErrIOTransient, fmt.Sprintf("scheduler queue full: job_id=%s", event.JobID))
	}
}

func (d *Dispatcher) TriggerNow(ctx context.Context, jobID api.JobID, reason api.TriggerReason) (api.RunID, error) {
	d.mu.RLock()
	started := d.started
	state, exists := d.jobs[jobID]
	d.mu.RUnlock()

	if !started {
		return "", api.Wrap(api.ErrInternal, "scheduler not started")
	}
	if !exists {
		return "", api.Wrap(api.ErrJobNotFound, fmt.Sprintf("job_id=%s", jobID))
	}

	req := manualRequest{
		ctx:      ctx,
		reason:   reason,
		response: make(chan triggerResult, 1),
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case state.manualCh <- req:
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-req.response:
		return res.runID, res.err
	}
}

func (d *Dispatcher) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.started {
		return nil
	}

	d.rootCtx, d.cancel = context.WithCancel(ctx)
	d.started = true

	for _, state := range d.jobs {
		d.startJobLocked(state)
	}

	if d.pendingStore != nil {
		all, err := d.pendingStore.LoadAll()
		if err != nil {
			d.logger.Warn("load pending events failed", api.Field{Key: "error", Value: err.Error()})
		} else {
			for jobID, events := range all {
				state, ok := d.jobs[jobID]
				if !ok {
					continue
				}
				for _, event := range events {
					select {
					case state.eventCh <- event:
					default:
						d.logger.Warn("recover pending event dropped", api.Field{Key: "job_id", Value: jobID})
					}
				}
			}
		}
	}
	return nil
}

func (d *Dispatcher) Stop(ctx context.Context) error {
	d.mu.Lock()
	if !d.started {
		d.mu.Unlock()
		return nil
	}
	d.started = false
	if d.cancel != nil {
		d.cancel()
	}

	states := make([]*jobState, 0, len(d.jobs))
	for _, state := range d.jobs {
		states = append(states, state)
	}
	d.mu.Unlock()

	for _, state := range states {
		d.stopJob(ctx, state)
	}
	return nil
}

func (d *Dispatcher) startJobLocked(state *jobState) {
	if state.running {
		return
	}
	jobCtx, cancel := context.WithCancel(d.rootCtx)
	state.cancel = cancel
	state.running = true
	go d.runJob(jobCtx, state)
}

func (d *Dispatcher) stopJob(ctx context.Context, state *jobState) {
	if state.cancel != nil {
		state.cancel()
	}
	select {
	case <-state.doneCh:
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
	}
}

func (d *Dispatcher) debounceFor(jobID api.JobID) time.Duration {
	cfg, ok := d.configs[jobID]
	if ok && cfg.debounce > 0 {
		return cfg.debounce
	}
	return time.Duration(api.DefaultEventDebounceMS) * time.Millisecond
}

func (d *Dispatcher) reconcileEnabledFor(jobID api.JobID) bool {
	cfg, ok := d.configs[jobID]
	if !ok {
		return api.DefaultPeriodicReconcileEnabled
	}
	return cfg.reconcileEnabled
}

func (d *Dispatcher) reconcileEveryFor(jobID api.JobID) time.Duration {
	cfg, ok := d.configs[jobID]
	if ok && cfg.reconcileInterval > 0 {
		return cfg.reconcileInterval
	}
	return time.Duration(api.DefaultPeriodicIntervalMinutes) * time.Minute
}

func (d *Dispatcher) runJob(ctx context.Context, state *jobState) {
	defer close(state.doneCh)

	pending := make(map[string]api.FileEvent)
	var timer *time.Timer
	var timerC <-chan time.Time
	var reconcileTicker *time.Ticker
	var reconcileC <-chan time.Time

	if state.reconcileEnabled {
		reconcileTicker = time.NewTicker(state.reconcileEvery)
		reconcileC = reconcileTicker.C
		defer reconcileTicker.Stop()
	}

	resetTimer := func() {
		if timer == nil {
			timer = time.NewTimer(state.debounce)
			timerC = timer.C
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(state.debounce)
		timerC = timer.C
	}

	flushPending := func() []api.FileEvent {
		if len(pending) == 0 {
			return nil
		}
		out := make([]api.FileEvent, 0, len(pending))
		for _, e := range pending {
			out = append(out, e)
		}
		pending = make(map[string]api.FileEvent)
		return out
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-state.eventCh:
			key := aggregateKey(event)
			pending[key] = event
			resetTimer()
		case req := <-state.manualCh:
			runID, err := d.runManualSync(req.ctx, state.id, req.reason)
			req.response <- triggerResult{runID: runID, err: err}
		case <-timerC:
			timerC = nil
			events := flushPending()
			if len(events) == 0 {
				continue
			}
			if d.pendingStore != nil {
				if err := d.pendingStore.Set(state.id, events); err != nil {
					d.logger.Warn("persist event batch failed", api.Field{Key: "job_id", Value: state.id}, api.Field{Key: "error", Value: err.Error()})
				}
			}
			if err := d.runEventSync(ctx, state.id, events); err != nil {
				d.logger.Error(
					"event sync failed",
					err,
					api.Field{Key: "job_id", Value: state.id},
					api.Field{Key: "event_count", Value: len(events)},
				)
				continue
			}
			if d.pendingStore != nil {
				if err := d.pendingStore.Clear(state.id); err != nil {
					d.logger.Warn("clear pending event batch failed", api.Field{Key: "job_id", Value: state.id}, api.Field{Key: "error", Value: err.Error()})
				}
			}
		case <-reconcileC:
			if err := d.runReconcile(ctx, state.id); err != nil {
				d.logger.Error(
					"periodic reconcile failed",
					err,
					api.Field{Key: "job_id", Value: state.id},
				)
			}
		}
	}
}

func (d *Dispatcher) runManualSync(ctx context.Context, jobID api.JobID, reason api.TriggerReason) (api.RunID, error) {
	runID := newRunID()
	_, err := d.backup.SyncNow(ctx, api.SyncRequest{
		JobID:       jobID,
		RequestID:   api.RequestID(runID),
		Reason:      reason,
		Mode:        api.SyncModeFull,
		RequestedAt: time.Now(),
	})
	if err != nil {
		return runID, err
	}
	return runID, nil
}

func (d *Dispatcher) runEventSync(ctx context.Context, jobID api.JobID, events []api.FileEvent) error {
	_, err := d.backup.SyncByEvents(ctx, jobID, events, api.TriggerFileEvent)
	if err == nil {
		return nil
	}

	if errors.Is(err, api.ErrNotImplemented) {
		d.logger.Warn(
			"incremental sync not implemented, fallback to full sync",
			api.Field{Key: "job_id", Value: jobID},
			api.Field{Key: "event_count", Value: len(events)},
		)
		_, fallbackErr := d.backup.SyncNow(ctx, api.SyncRequest{
			JobID:        jobID,
			RequestID:    api.RequestID(newRunID()),
			Reason:       api.TriggerFileEvent,
			Mode:         api.SyncModeFull,
			ChangedPaths: collectChangedPaths(events),
			RequestedAt:  time.Now(),
		})
		return fallbackErr
	}
	return err
}

func (d *Dispatcher) runReconcile(ctx context.Context, jobID api.JobID) error {
	_, err := d.backup.Reconcile(ctx, jobID)
	return err
}

func collectChangedPaths(events []api.FileEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Path)
	}
	return out
}

func aggregateKey(event api.FileEvent) string {
	return fmt.Sprintf("%s|%s", event.Path, event.Op)
}

func newRunID() api.RunID {
	return api.RunID(fmt.Sprintf("run-%d", time.Now().UnixNano()))
}
