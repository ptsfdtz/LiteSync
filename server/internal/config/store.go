package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"litesync/server/internal/model"
)

const fileName = "config.json"

type Store struct {
	mu       sync.Mutex
	dataDir  string
	filePath string
}

func NewStore(dataDir string) *Store {
	return &Store{
		dataDir:  dataDir,
		filePath: filepath.Join(dataDir, fileName),
	}
}

func (s *Store) Load() (model.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.DefaultConfig(), nil
		}
		return model.Config{}, err
	}

	cfg := model.DefaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return model.Config{}, err
	}

	return normalize(cfg), nil
}

func (s *Store) Save(cfg model.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg = normalize(cfg)

	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, payload, 0o644)
}

func normalize(cfg model.Config) model.Config {
	cfg.SourceDir = cleanPath(cfg.SourceDir)
	cfg.TargetDir = cleanPath(cfg.TargetDir)

	if cfg.IntervalMinutes <= 0 {
		cfg.IntervalMinutes = 60
	}

	return cfg
}

func cleanPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}

	return filepath.Clean(trimmed)
}
