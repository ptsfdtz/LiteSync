package scheduler

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"litesync/internal/api"
	"litesync/internal/logx"
)

type backupFake struct {
	mu sync.Mutex

	syncByEventsCalls int
	syncNowCalls      int
	reconcileCalls    int
	lastEvents        []api.FileEvent
	lastSyncNowMode   api.SyncMode

	syncByEventsErr      error
	syncByEventsErrByJob map[api.JobID]error
	blockEventsCh        chan struct{}
	firstEventRunCh      chan struct{}

	activeRuns int
	maxActive  int
}

type pendingStoreFake struct {
	mu   sync.Mutex
	data map[api.JobID][]api.FileEvent
}

func newPendingStoreFake() *pendingStoreFake {
	return &pendingStoreFake{
		data: make(map[api.JobID][]api.FileEvent),
	}
}

func (s *pendingStoreFake) LoadAll() (map[api.JobID][]api.FileEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[api.JobID][]api.FileEvent, len(s.data))
	for k, v := range s.data {
		cloned := make([]api.FileEvent, len(v))
		copy(cloned, v)
		out[k] = cloned
	}
	return out, nil
}

func (s *pendingStoreFake) Add(event api.FileEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[event.JobID] = append(s.data[event.JobID], event)
	return nil
}

func (s *pendingStoreFake) Set(jobID api.JobID, events []api.FileEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(events) == 0 {
		delete(s.data, jobID)
		return nil
	}
	cloned := make([]api.FileEvent, len(events))
	copy(cloned, events)
	s.data[jobID] = cloned
	return nil
}

func (s *pendingStoreFake) Clear(jobID api.JobID) error {
	return s.Set(jobID, nil)
}

func (f *backupFake) SyncNow(_ context.Context, req api.SyncRequest) (api.SyncResult, error) {
	f.mu.Lock()
	f.syncNowCalls++
	f.lastSyncNowMode = req.Mode
	f.mu.Unlock()
	return api.SyncResult{JobID: req.JobID, RunID: api.RunID(req.RequestID)}, nil
}

func (f *backupFake) SyncByEvents(_ context.Context, jobID api.JobID, events []api.FileEvent, _ api.TriggerReason) (api.SyncResult, error) {
	f.mu.Lock()
	f.syncByEventsCalls++
	f.lastEvents = append([]api.FileEvent(nil), events...)
	f.activeRuns++
	if f.activeRuns > f.maxActive {
		f.maxActive = f.activeRuns
	}
	if f.syncByEventsCalls == 1 && f.firstEventRunCh != nil {
		close(f.firstEventRunCh)
		f.firstEventRunCh = nil
	}
	blockCh := f.blockEventsCh
	err := f.syncByEventsErr
	if f.syncByEventsErrByJob != nil {
		if byJobErr, ok := f.syncByEventsErrByJob[jobID]; ok {
			err = byJobErr
		}
	}
	f.mu.Unlock()

	if blockCh != nil {
		<-blockCh
	}
	time.Sleep(30 * time.Millisecond)

	f.mu.Lock()
	f.activeRuns--
	f.mu.Unlock()

	if err != nil {
		return api.SyncResult{JobID: jobID}, err
	}
	return api.SyncResult{JobID: jobID}, nil
}

func (f *backupFake) Reconcile(_ context.Context, jobID api.JobID) (api.SyncResult, error) {
	f.mu.Lock()
	f.reconcileCalls++
	f.mu.Unlock()
	return api.SyncResult{JobID: jobID}, nil
}

func (f *backupFake) Cancel(_ context.Context, _ api.RunID) error {
	return nil
}

func TestDebounceEventAggregation(t *testing.T) {
	fake := &backupFake{}
	logger := logx.NewWithWriter("debug", io.Discard)
	d := New(fake, logger)

	d.ConfigureJobs([]api.Job{
		{
			ID: "job-1",
			Strategy: api.Strategy{
				EventSync: api.EventSync{DebounceMS: 120},
			},
		},
	})

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	if err := d.RegisterJob(context.Background(), "job-1"); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	if err := d.PushEvent(context.Background(), api.FileEvent{JobID: "job-1", Path: "/tmp/a.txt", Op: api.FileWrite}); err != nil {
		t.Fatalf("push event 1 failed: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if err := d.PushEvent(context.Background(), api.FileEvent{JobID: "job-1", Path: "/tmp/b.txt", Op: api.FileWrite}); err != nil {
		t.Fatalf("push event 2 failed: %v", err)
	}

	waitUntil(t, 2*time.Second, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.syncByEventsCalls == 1
	})

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.lastEvents) < 2 {
		t.Fatalf("expected at least 2 aggregated events, got %d", len(fake.lastEvents))
	}
}

func TestSingleJobNoConcurrentRuns(t *testing.T) {
	blockCh := make(chan struct{})
	firstRunCh := make(chan struct{})
	fake := &backupFake{
		blockEventsCh:   blockCh,
		firstEventRunCh: firstRunCh,
	}
	logger := logx.NewWithWriter("debug", io.Discard)
	d := New(fake, logger)

	d.ConfigureJobs([]api.Job{
		{
			ID: "job-2",
			Strategy: api.Strategy{
				EventSync: api.EventSync{DebounceMS: 30},
			},
		},
	})

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	if err := d.RegisterJob(context.Background(), "job-2"); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	if err := d.PushEvent(context.Background(), api.FileEvent{JobID: "job-2", Path: "/tmp/a.txt", Op: api.FileWrite}); err != nil {
		t.Fatalf("push event 1 failed: %v", err)
	}

	select {
	case <-firstRunCh:
	case <-time.After(2 * time.Second):
		t.Fatal("first run did not start")
	}

	if err := d.PushEvent(context.Background(), api.FileEvent{JobID: "job-2", Path: "/tmp/b.txt", Op: api.FileWrite}); err != nil {
		t.Fatalf("push event 2 failed: %v", err)
	}

	close(blockCh)

	waitUntil(t, 2*time.Second, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.syncByEventsCalls >= 2
	})

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.maxActive > 1 {
		t.Fatalf("expected max active runs <= 1, got %d", fake.maxActive)
	}
}

func TestTriggerNow(t *testing.T) {
	fake := &backupFake{}
	logger := logx.NewWithWriter("debug", io.Discard)
	d := New(fake, logger)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	if err := d.RegisterJob(context.Background(), "job-3"); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	runID, err := d.TriggerNow(context.Background(), "job-3", api.TriggerManual)
	if err != nil {
		t.Fatalf("trigger now failed: %v", err)
	}
	if runID == "" {
		t.Fatalf("expected non-empty runID")
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.syncNowCalls != 1 {
		t.Fatalf("expected syncNow calls=1, got %d", fake.syncNowCalls)
	}
	if fake.lastSyncNowMode != api.SyncModeFull {
		t.Fatalf("expected sync mode full, got %s", fake.lastSyncNowMode)
	}
}

func TestFallbackToFullSyncWhenIncrementalNotImplemented(t *testing.T) {
	fake := &backupFake{syncByEventsErr: api.ErrNotImplemented}
	logger := logx.NewWithWriter("debug", io.Discard)
	d := New(fake, logger)

	d.ConfigureJobs([]api.Job{
		{
			ID: "job-4",
			Strategy: api.Strategy{
				EventSync: api.EventSync{DebounceMS: 50},
			},
		},
	})

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	if err := d.RegisterJob(context.Background(), "job-4"); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	if err := d.PushEvent(context.Background(), api.FileEvent{JobID: "job-4", Path: "/tmp/a.txt", Op: api.FileWrite}); err != nil {
		t.Fatalf("push event failed: %v", err)
	}

	waitUntil(t, 2*time.Second, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.syncByEventsCalls >= 1 && fake.syncNowCalls >= 1
	})
}

func waitUntil(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !check() {
		t.Fatal("condition not met before timeout")
	}
}

func TestTriggerNowJobNotFound(t *testing.T) {
	fake := &backupFake{}
	logger := logx.NewWithWriter("debug", io.Discard)
	d := New(fake, logger)

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	_, err := d.TriggerNow(context.Background(), "missing", api.TriggerManual)
	if !errors.Is(err, api.ErrJobNotFound) {
		t.Fatalf("expected job not found error, got %v", err)
	}
}

func TestPeriodicReconcile(t *testing.T) {
	fake := &backupFake{}
	logger := logx.NewWithWriter("debug", io.Discard)
	d := New(fake, logger)

	d.ConfigureJobs([]api.Job{
		{
			ID: "job-rc",
			Strategy: api.Strategy{
				EventSync: api.EventSync{DebounceMS: 50},
				PeriodicReconcile: api.PeriodicReconcile{
					Enabled:         true,
					IntervalMinutes: 0,
				},
			},
		},
	})

	if err := d.RegisterJob(context.Background(), "job-rc"); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// Override interval to keep test short.
	d.mu.Lock()
	if st, ok := d.jobs["job-rc"]; ok {
		st.reconcileEnabled = true
		st.reconcileEvery = 100 * time.Millisecond
	}
	d.mu.Unlock()

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	waitUntil(t, 2*time.Second, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.reconcileCalls >= 1
	})
}

func TestMultiJobIsolation(t *testing.T) {
	fake := &backupFake{
		syncByEventsErrByJob: map[api.JobID]error{
			"job-a": api.ErrIOTransient,
		},
	}
	logger := logx.NewWithWriter("debug", io.Discard)
	d := New(fake, logger)

	d.ConfigureJobs([]api.Job{
		{
			ID: "job-a",
			Strategy: api.Strategy{
				EventSync: api.EventSync{DebounceMS: 60},
			},
		},
		{
			ID: "job-b",
			Strategy: api.Strategy{
				EventSync: api.EventSync{DebounceMS: 60},
			},
		},
	})

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	if err := d.RegisterJob(context.Background(), "job-a"); err != nil {
		t.Fatalf("register job-a failed: %v", err)
	}
	if err := d.RegisterJob(context.Background(), "job-b"); err != nil {
		t.Fatalf("register job-b failed: %v", err)
	}

	if err := d.PushEvent(context.Background(), api.FileEvent{JobID: "job-a", Path: "/tmp/a.txt", Op: api.FileWrite}); err != nil {
		t.Fatalf("push event job-a failed: %v", err)
	}
	if err := d.PushEvent(context.Background(), api.FileEvent{JobID: "job-b", Path: "/tmp/b.txt", Op: api.FileWrite}); err != nil {
		t.Fatalf("push event job-b failed: %v", err)
	}

	waitUntil(t, 2*time.Second, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.syncByEventsCalls >= 2
	})
}

func TestRecoveryReplayPendingEvents(t *testing.T) {
	fake := &backupFake{}
	store := newPendingStoreFake()
	store.data["job-r"] = []api.FileEvent{
		{JobID: "job-r", Path: "/tmp/pending.txt", Op: api.FileWrite, OccurredAt: time.Now()},
	}

	logger := logx.NewWithWriter("debug", io.Discard)
	d := New(fake, logger)
	d.EnableRecovery(store)
	d.ConfigureJobs([]api.Job{
		{
			ID: "job-r",
			Strategy: api.Strategy{
				EventSync: api.EventSync{DebounceMS: 50},
			},
		},
	})

	if err := d.RegisterJob(context.Background(), "job-r"); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	waitUntil(t, 2*time.Second, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.syncByEventsCalls >= 1
	})
}
