package state

import (
	"encoding/json"
	"os"
	"path/filepath"

	"litesync/internal/api"
)

const RuntimeFileName = "runtime_state.json"

type FileStore struct {
	dir string
}

func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: filepath.Clean(dir)}
}

func (s *FileStore) Save(snapshot api.RuntimeSnapshot) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return api.Wrap(api.ErrPermissionDenied, "create state dir failed")
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return api.Wrap(api.ErrInternal, "encode runtime snapshot failed")
	}

	path := filepath.Join(s.dir, RuntimeFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return api.Wrap(api.ErrIOTransient, "write state temp file failed")
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return api.Wrap(api.ErrIOTransient, "replace state file failed")
	}
	return nil
}

func (s *FileStore) Load() (api.RuntimeSnapshot, error) {
	path := filepath.Join(s.dir, RuntimeFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return api.RuntimeSnapshot{}, nil
	}
	if err != nil {
		return api.RuntimeSnapshot{}, api.Wrap(api.ErrIOTransient, "read state file failed")
	}

	var snapshot api.RuntimeSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return api.RuntimeSnapshot{}, api.Wrap(api.ErrInvalidArgument, "decode state file failed")
	}
	return snapshot, nil
}
