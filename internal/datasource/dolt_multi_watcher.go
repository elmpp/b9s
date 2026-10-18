package datasource

import (
	"context"
	"sync"
	"time"

	"github.com/vanderheijden86/b9s/pkg/debug"
)

// MultiDoltWatcher polls working-set content hashes across multiple Dolt
// databases and fires a change notification when any database changes.
type MultiDoltWatcher struct {
	reader       *MultiDoltReader
	pollInterval time.Duration
	lastHashes   map[string]string
	activity     *Activity

	ctx      context.Context
	cancel   context.CancelFunc
	changeCh chan struct{}
	started  bool
	mu       sync.RWMutex
}

// NewMultiDoltWatcher creates a watcher that monitors all databases in the reader.
func NewMultiDoltWatcher(reader *MultiDoltReader, pollInterval time.Duration) *MultiDoltWatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &MultiDoltWatcher{
		reader:       reader,
		pollInterval: pollInterval,
		lastHashes:   make(map[string]string),
		changeCh:     make(chan struct{}, 1),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start begins polling. Captures initial hashes, then polls for changes.
func (w *MultiDoltWatcher) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started {
		return nil
	}

	// Capture initial hashes
	w.lastHashes = w.reader.GetDatabaseHashes()
	debug.Log("multi-dolt-watcher: initial hashes for %d databases", len(w.lastHashes))

	w.started = true
	go w.poll()
	return nil
}

// FollowActivity makes the watcher back off while the user is idle, as
// DoltWatcher.FollowActivity does.
func (w *MultiDoltWatcher) FollowActivity(a *Activity) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.activity = a
}

func (w *MultiDoltWatcher) nextPoll() (time.Duration, <-chan struct{}) {
	w.mu.RLock()
	a := w.activity
	w.mu.RUnlock()
	if a == nil {
		return w.pollInterval, nil
	}
	return a.Interval(w.pollInterval), a.Woken()
}

// WaitForChange blocks until any database changes or the context is cancelled.
func (w *MultiDoltWatcher) WaitForChange() bool {
	select {
	case <-w.changeCh:
		return true
	case <-w.ctx.Done():
		return false
	}
}

// Stop stops the watcher.
func (w *MultiDoltWatcher) Stop() {
	w.cancel()
}

func (w *MultiDoltWatcher) poll() {
	timer := time.NewTimer(w.pollInterval)
	defer timer.Stop()

	for {
		interval, wake := w.nextPoll()
		timer.Reset(interval)

		select {
		case <-w.ctx.Done():
			return
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
		w.check()
	}
}

func (w *MultiDoltWatcher) check() {
	hashes := w.reader.GetDatabaseHashes()
	changed := false
	for db, hash := range hashes {
		if old, ok := w.lastHashes[db]; !ok || old != hash {
			debug.Log("multi-dolt-watcher: %s changed %s -> %s", db, old, hash)
			changed = true
		}
	}
	if changed {
		w.mu.Lock()
		w.lastHashes = hashes
		w.mu.Unlock()
		select {
		case w.changeCh <- struct{}{}:
		default:
		}
	}
}
