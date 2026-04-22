package watcher

import (
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Handler is called when a new .xlsx file is detected.
type Handler func(path string)

// Watcher monitors a folder for new Excel files.
type Watcher struct {
	folder    string
	handler   Handler
	seen      map[string]bool
	mu        sync.Mutex
	stop      chan struct{}
}

// New creates a new folder watcher.
func New(folder string, handler Handler) *Watcher {
	return &Watcher{
		folder:  folder,
		handler: handler,
		seen:    make(map[string]bool),
		stop:    make(chan struct{}),
	}
}

// MarkExisting marks all current files as already processed.
func (w *Watcher) MarkExisting() {
	matches, _ := filepath.Glob(filepath.Join(w.folder, "*.xlsx"))
	w.mu.Lock()
	for _, m := range matches {
		w.seen[filepath.Base(m)] = true
	}
	w.mu.Unlock()
	log.Printf("[watcher] Marked %d existing files", len(matches))
}

// Start begins monitoring the folder.
func (w *Watcher) Start() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	if err := watcher.Add(w.folder); err != nil {
		watcher.Close()
		return err
	}

	log.Printf("[watcher] Monitoring folder: %s", w.folder)

	go func() {
		defer watcher.Close()
		// Debounce map to avoid processing the same file multiple times
		pending := map[string]time.Time{}
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-w.stop:
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !strings.HasSuffix(strings.ToLower(event.Name), ".xlsx") {
					continue
				}
				// Skip GDrive temp files (sync_tmp_N.xlsx) — they are
				// renamed to their final name before we should process them.
				// Firing on the temp name causes "no such file" parse errors.
				if strings.HasPrefix(filepath.Base(event.Name), "sync_tmp_") {
					continue
				}
				if event.Op&(fsnotify.Create|fsnotify.Write) == 0 {
					continue
				}
				name := filepath.Base(event.Name)
				w.mu.Lock()
				if !w.seen[name] {
					pending[event.Name] = time.Now()
				}
				w.mu.Unlock()

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("[watcher] Error: %v", err)

			case <-ticker.C:
				// Process pending files that have settled (2s since last event)
				now := time.Now()
				for path, ts := range pending {
					if now.Sub(ts) >= 2*time.Second {
						name := filepath.Base(path)
						w.mu.Lock()
						w.seen[name] = true
						w.mu.Unlock()
						delete(pending, path)
						log.Printf("[watcher] New file: %s", name)
						go w.handler(path)
					}
				}
			}
		}
	}()

	return nil
}

// Stop terminates the watcher.
func (w *Watcher) Stop() {
	close(w.stop)
}
