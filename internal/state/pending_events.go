package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"litesync/internal/api"
)

const PendingEventsFileName = "pending_events.json"

type PendingEventStore struct {
	dir string
	mu  sync.Mutex
}

func NewPendingEventStore(dir string) *PendingEventStore {
	return &PendingEventStore{dir: filepath.Clean(dir)}
}

func (s *PendingEventStore) LoadAll() (map[api.JobID][]api.FileEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadAllLocked()
}

func (s *PendingEventStore) Add(event api.FileEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.loadAllLocked()
	if err != nil {
		return err
	}
	all[event.JobID] = append(all[event.JobID], event)
	return s.saveAllLocked(all)
}

func (s *PendingEventStore) Set(jobID api.JobID, events []api.FileEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.loadAllLocked()
	if err != nil {
		return err
	}
	if len(events) == 0 {
		delete(all, jobID)
	} else {
		cloned := make([]api.FileEvent, len(events))
		copy(cloned, events)
		all[jobID] = cloned
	}
	return s.saveAllLocked(all)
}

func (s *PendingEventStore) Clear(jobID api.JobID) error {
	return s.Set(jobID, nil)
}

func (s *PendingEventStore) loadAllLocked() (map[api.JobID][]api.FileEvent, error) {
	path := filepath.Join(s.dir, PendingEventsFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[api.JobID][]api.FileEvent{}, nil
	}
	if err != nil {
		return nil, api.Wrap(api.ErrIOTransient, "read pending events failed")
	}

	var raw map[string][]api.FileEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, api.Wrap(api.ErrInvalidArgument, "decode pending events failed")
	}

	out := make(map[api.JobID][]api.FileEvent, len(raw))
	for k, v := range raw {
		out[api.JobID(k)] = v
	}
	return out, nil
}

func (s *PendingEventStore) saveAllLocked(all map[api.JobID][]api.FileEvent) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return api.Wrap(api.ErrPermissionDenied, "create state dir failed")
	}

	raw := make(map[string][]api.FileEvent, len(all))
	for k, v := range all {
		raw[string(k)] = v
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return api.Wrap(api.ErrInternal, "encode pending events failed")
	}

	path := filepath.Join(s.dir, PendingEventsFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return api.Wrap(api.ErrIOTransient, "write pending events temp failed")
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return api.Wrap(api.ErrIOTransient, "replace pending events file failed")
	}
	return nil
}
