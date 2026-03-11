package watcher

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	debounceWindow time.Duration
}

func New(debounceWindow time.Duration) *Watcher {
	if debounceWindow <= 0 {
		debounceWindow = 2 * time.Second
	}

	return &Watcher{
		debounceWindow: debounceWindow,
	}
}

func (w *Watcher) Run(ctx context.Context, sourceDir string, onChange func()) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fs watcher: %w", err)
	}
	defer watcher.Close()

	if err := addDirectoryRecursive(watcher, sourceDir); err != nil {
		return fmt.Errorf("watch source directory: %w", err)
	}

	var timer *time.Timer
	var timerChannel <-chan time.Time

	scheduleTrigger := func() {
		if timer == nil {
			timer = time.NewTimer(w.debounceWindow)
			timerChannel = timer.C
			return
		}

		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(w.debounceWindow)
	}

	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, open := <-watcher.Events:
			if !open {
				return nil
			}

			if event.Op&fsnotify.Create != 0 {
				if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
					_ = addDirectoryRecursive(watcher, event.Name)
				}
			}

			if !isMeaningfulEvent(event.Op) {
				continue
			}

			scheduleTrigger()
		case <-timerChannel:
			timerChannel = nil
			onChange()
		case _, open := <-watcher.Errors:
			if !open {
				return nil
			}
		}
	}
}

func addDirectoryRecursive(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !entry.IsDir() {
			return nil
		}

		return watcher.Add(path)
	})
}

func isMeaningfulEvent(op fsnotify.Op) bool {
	return op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0
}
