package logs

import (
	"sync"
	"time"

	"litesync/server/internal/model"
)

type Buffer struct {
	mu      sync.RWMutex
	maxSize int
	entries []model.LogEntry
}

func NewBuffer(maxSize int) *Buffer {
	if maxSize <= 0 {
		maxSize = 1
	}

	return &Buffer{
		maxSize: maxSize,
		entries: make([]model.LogEntry, 0, maxSize),
	}
}

func (b *Buffer) Add(level string, message string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.entries = append(b.entries, model.LogEntry{
		Time:    time.Now().UTC(),
		Level:   level,
		Message: message,
	})

	if overflow := len(b.entries) - b.maxSize; overflow > 0 {
		b.entries = append([]model.LogEntry(nil), b.entries[overflow:]...)
	}
}

func (b *Buffer) List(limit int) []model.LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if limit <= 0 || limit > len(b.entries) {
		limit = len(b.entries)
	}

	result := make([]model.LogEntry, 0, limit)
	for i := len(b.entries) - 1; i >= 0 && len(result) < limit; i-- {
		result = append(result, b.entries[i])
	}

	return result
}
