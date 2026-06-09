package devreload

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Reloader struct {
	mu          sync.Mutex
	subscribers map[chan string]struct{}
	log         *slog.Logger
}

func Enabled() bool {
	value := strings.ToLower(os.Getenv("RELAY_DEV_RELOAD"))
	return value == "1" || value == "true" || value == "yes"
}

func New(log *slog.Logger) *Reloader {
	return &Reloader{
		subscribers: make(map[chan string]struct{}),
		log:         log,
	}
}

func (r *Reloader) broadcast(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for ch := range r.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func (r *Reloader) subscribe() chan string {
	ch := make(chan string, 1)
	r.mu.Lock()
	r.subscribers[ch] = struct{}{}
	r.mu.Unlock()
	return ch
}

func (r *Reloader) unsubscribe(ch chan string) {
	r.mu.Lock()
	delete(r.subscribers, ch)
	r.mu.Unlock()
}

func (r *Reloader) Handler(w http.ResponseWriter, req *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher.Flush()

	ch := r.subscribe()
	defer r.unsubscribe(ch)

	fmt.Fprintf(w, ": ok\n\n")
	flusher.Flush()

	for {
		select {
		case event := <-ch:
			fmt.Fprintf(w, "event: %s\ndata: \n\n", event)
			flusher.Flush()
		case <-req.Context().Done():
			return
		}
	}
}

func (r *Reloader) Watch(paths ...string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}

	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			watcher.Close()
			return fmt.Errorf("abs path %s: %w", p, err)
		}
		if err := watcher.Add(abs); err != nil {
			watcher.Close()
			return fmt.Errorf("watch %s: %w", p, err)
		}
	}

	go func() {
		defer watcher.Close()

		debounce := time.NewTimer(0)
		if !debounce.Stop() {
			<-debounce.C
		}
		debounceC := debounce.C

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
					name := filepath.Base(event.Name)
					if name == "app.css" || name == "app.js" {
						if debounceC != nil {
							debounce.Stop()
						}
						debounce.Reset(100 * time.Millisecond)
					}
				}
			case <-debounceC:
				r.broadcast("reload")
				debounceC = nil
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				r.log.Warn("fsnotify watcher error", "error", err)
			}
		}
	}()

	return nil
}
