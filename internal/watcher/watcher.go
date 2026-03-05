package watcher

import (
	"context"
	"path/filepath"
	"sync"

	"litesync/internal/api"
)

// NoopWatcher 是监听模块的占位实现。
type NoopWatcher struct {
	mu      sync.RWMutex
	running map[api.JobID]string
	events  chan api.FileEvent
	errs    chan error
}

func New() *NoopWatcher {
	return &NoopWatcher{
		running: make(map[api.JobID]string),
		events:  make(chan api.FileEvent, 128),
		errs:    make(chan error, 16),
	}
}

func (w *NoopWatcher) Start(_ context.Context, jobID api.JobID, sourceDir string) error {
	if sourceDir == "" || !filepath.IsAbs(sourceDir) {
		return api.Wrap(api.ErrInvalidArgument, "sourceDir must be an absolute path")
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.running[jobID]; ok {
		return nil
	}
	w.running[jobID] = filepath.Clean(sourceDir)
	return nil
}

func (w *NoopWatcher) Stop(_ context.Context, jobID api.JobID) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.running, jobID)
	return nil
}

func (w *NoopWatcher) Events() <-chan api.FileEvent {
	return w.events
}

func (w *NoopWatcher) Errors() <-chan error {
	return w.errs
}
