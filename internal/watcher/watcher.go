package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"litesync/internal/api"
)

type Service struct {
	mu     sync.RWMutex
	jobs   map[api.JobID]*jobWatcher
	events chan api.FileEvent
	errs   chan error
}

type jobWatcher struct {
	id      api.JobID
	root    string
	w       *fsnotify.Watcher
	mu      sync.RWMutex
	watched map[string]struct{}
	cancel  context.CancelFunc
	done    chan struct{}
}

func New() *Service {
	return &Service{
		jobs:   make(map[api.JobID]*jobWatcher),
		events: make(chan api.FileEvent, 1024),
		errs:   make(chan error, 128),
	}
}

func (s *Service) Start(ctx context.Context, jobID api.JobID, sourceDir string) error {
	if sourceDir == "" || !filepath.IsAbs(sourceDir) {
		return api.Wrap(api.ErrInvalidArgument, "sourceDir must be an absolute path")
	}

	root := filepath.Clean(sourceDir)
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return api.Wrap(api.ErrInvalidArgument, fmt.Sprintf("sourceDir does not exist: %s", root))
		}
		if os.IsPermission(err) {
			return api.Wrap(api.ErrPermissionDenied, fmt.Sprintf("cannot access sourceDir: %s", root))
		}
		return api.Wrap(api.ErrIOTransient, fmt.Sprintf("stat sourceDir failed: %v", err))
	}
	if !info.IsDir() {
		return api.Wrap(api.ErrInvalidArgument, "sourceDir must be a directory")
	}

	s.mu.Lock()
	if _, ok := s.jobs[jobID]; ok {
		s.mu.Unlock()
		return nil
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		s.mu.Unlock()
		return api.Wrap(api.ErrInternal, fmt.Sprintf("create fsnotify watcher failed: %v", err))
	}
	jobCtx, cancel := context.WithCancel(ctx)
	jw := &jobWatcher{
		id:      jobID,
		root:    root,
		w:       fsw,
		watched: make(map[string]struct{}),
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	if err := s.addRecursive(jw, root); err != nil {
		s.mu.Unlock()
		cancel()
		_ = fsw.Close()
		return err
	}
	s.jobs[jobID] = jw
	s.mu.Unlock()

	go s.run(jobCtx, jw)
	return nil
}

func (s *Service) Stop(ctx context.Context, jobID api.JobID) error {
	s.mu.Lock()
	jw, ok := s.jobs[jobID]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	delete(s.jobs, jobID)
	s.mu.Unlock()

	jw.cancel()
	_ = jw.w.Close()

	select {
	case <-jw.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return api.Wrap(api.ErrIOTransient, "watcher stop timeout")
	}
}

func (s *Service) Events() <-chan api.FileEvent {
	return s.events
}

func (s *Service) Errors() <-chan error {
	return s.errs
}

func (s *Service) run(ctx context.Context, jw *jobWatcher) {
	defer close(jw.done)
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-jw.w.Events:
			if !ok {
				return
			}
			s.handleEvent(jw, event)
		case err, ok := <-jw.w.Errors:
			if !ok {
				return
			}
			s.publishErr(api.Wrap(api.ErrIOTransient, fmt.Sprintf("watcher job_id=%s: %v", jw.id, err)))
		}
	}
}

func (s *Service) handleEvent(jw *jobWatcher, e fsnotify.Event) {
	eventPath := filepath.Clean(e.Name)
	isDir := jw.isKnownDir(eventPath)

	if e.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(eventPath); err == nil && info.IsDir() {
			isDir = true
			if err := s.addRecursive(jw, eventPath); err != nil {
				s.publishErr(err)
			}
		}
	}

	if e.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		jw.removeWatchedPrefix(eventPath)
	}

	ops := mapFsnotifyOps(e.Op)
	for _, op := range ops {
		s.publishEvent(api.FileEvent{
			JobID:      jw.id,
			Path:       eventPath,
			Op:         op,
			IsDir:      isDir,
			OccurredAt: time.Now(),
		})
	}
}

func (s *Service) addRecursive(jw *jobWatcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsPermission(walkErr) {
				return api.Wrap(api.ErrPermissionDenied, fmt.Sprintf("watch path denied: %s", path))
			}
			return api.Wrap(api.ErrIOTransient, fmt.Sprintf("walk path failed: %v", walkErr))
		}
		if !d.IsDir() {
			return nil
		}
		return jw.addWatch(path)
	})
}

func (jw *jobWatcher) addWatch(dir string) error {
	dir = filepath.Clean(dir)

	jw.mu.Lock()
	defer jw.mu.Unlock()
	if _, ok := jw.watched[dir]; ok {
		return nil
	}
	if err := jw.w.Add(dir); err != nil {
		if os.IsPermission(err) {
			return api.Wrap(api.ErrPermissionDenied, fmt.Sprintf("watch add denied: %s", dir))
		}
		return api.Wrap(api.ErrIOTransient, fmt.Sprintf("watch add failed: %s: %v", dir, err))
	}
	jw.watched[dir] = struct{}{}
	return nil
}

func (jw *jobWatcher) removeWatchedPrefix(prefix string) {
	prefix = filepath.Clean(prefix)

	jw.mu.Lock()
	defer jw.mu.Unlock()
	for p := range jw.watched {
		if pathEqOrHasPrefix(p, prefix) {
			_ = jw.w.Remove(p)
			delete(jw.watched, p)
		}
	}
}

func (jw *jobWatcher) isKnownDir(path string) bool {
	path = filepath.Clean(path)

	if info, err := os.Stat(path); err == nil {
		return info.IsDir()
	}

	jw.mu.RLock()
	defer jw.mu.RUnlock()
	_, ok := jw.watched[path]
	return ok
}

func (s *Service) publishEvent(e api.FileEvent) {
	select {
	case s.events <- e:
	default:
		s.publishErr(api.Wrap(api.ErrIOTransient, fmt.Sprintf("event dropped: channel full, job_id=%s path=%s op=%s", e.JobID, e.Path, e.Op)))
	}
}

func (s *Service) publishErr(err error) {
	select {
	case s.errs <- err:
	default:
	}
}

func mapFsnotifyOps(op fsnotify.Op) []api.FileOp {
	out := make([]api.FileOp, 0, 5)
	if op&fsnotify.Create != 0 {
		out = append(out, api.FileCreate)
	}
	if op&fsnotify.Write != 0 {
		out = append(out, api.FileWrite)
	}
	if op&fsnotify.Remove != 0 {
		out = append(out, api.FileRemove)
	}
	if op&fsnotify.Rename != 0 {
		out = append(out, api.FileRename)
	}
	if op&fsnotify.Chmod != 0 {
		out = append(out, api.FileChmod)
	}
	return out
}

func pathEqOrHasPrefix(pathValue, prefix string) bool {
	p := filepath.Clean(pathValue)
	base := filepath.Clean(prefix)

	if pathEqual(p, base) {
		return true
	}

	if len(p) <= len(base) {
		return false
	}

	withSep := base + string(filepath.Separator)
	if runtime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(p), strings.ToLower(withSep))
	}
	return strings.HasPrefix(p, withSep)
}

func pathEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
