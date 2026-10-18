package datasource

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DoltWatcher polls a Dolt database for working-set content changes. It
// notifies callers for both committed and uncommitted writes.
type DoltWatcher struct {
	hash         func() (string, error)
	closeSource  func()
	pollInterval time.Duration
	lastHash     string
	onChange     func()
	activity     *Activity

	ctx      context.Context
	cancel   context.CancelFunc
	changeCh chan struct{}
	started  bool
	mu       sync.RWMutex
}

// NewDoltWatcher creates a DoltWatcher backed by the given DataSource. For a
// Dolt server it opens a DoltReader (verifying connectivity) and polls the
// database hash. For an embedded store it polls a fingerprint of the store
// files and holds nothing open, so bd writes never meet a lock held by b9s
// (ADR 0025). The caller must call Start to begin polling.
func NewDoltWatcher(source DataSource, pollInterval time.Duration) (*DoltWatcher, error) {
	var hash func() (string, error)
	closeSource := func() {}
	switch source.Type {
	case SourceTypeDolt:
		reader, err := NewDoltReader(source)
		if err != nil {
			return nil, fmt.Errorf("DoltWatcher: cannot open reader: %w", err)
		}
		hash = reader.GetDatabaseHash
		closeSource = func() { reader.Close() } //nolint:errcheck // best-effort on shutdown
	case SourceTypeDoltEmbedded:
		hash = func() (string, error) { return embeddedFingerprint(source.Path) }
	default:
		return nil, fmt.Errorf("DoltWatcher requires a Dolt source, got: %s", source.Type)
	}

	return newHashWatcher(hash, closeSource, pollInterval), nil
}

func newHashWatcher(hash func() (string, error), closeSource func(), pollInterval time.Duration) *DoltWatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &DoltWatcher{
		hash:         hash,
		closeSource:  closeSource,
		pollInterval: pollInterval,
		changeCh:     make(chan struct{}, 1),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// FollowActivity makes the watcher back off while the user is idle (see
// Activity). A watcher that follows nothing polls at its fixed interval,
// which is what the web server wants: it has no keyboard to be idle at.
func (w *DoltWatcher) FollowActivity(a *Activity) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.activity = a
}

// nextPoll returns how long to wait before the next poll and the channel that
// cuts that wait short when the user returns from idle.
func (w *DoltWatcher) nextPoll() (time.Duration, <-chan struct{}) {
	w.mu.RLock()
	a := w.activity
	w.mu.RUnlock()
	if a == nil {
		return w.pollInterval, nil
	}
	return a.Interval(w.pollInterval), a.Woken()
}

// Start captures the current database hash as a baseline and launches the
// background poll goroutine. Calling Start on an already-started watcher is a
// no-op.
func (w *DoltWatcher) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started {
		return nil
	}

	// Establish baseline hash — do not notify on first observation.
	hash, err := w.hash()
	if err != nil {
		// Non-fatal: proceed without a baseline; the first successful poll will
		// set it without triggering a spurious notification.
		hash = ""
	}
	w.lastHash = hash

	go w.poll()
	w.started = true
	return nil
}

// Stop cancels the poll goroutine and closes the underlying DoltReader. It is
// safe to call Stop multiple times.
func (w *DoltWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return
	}

	w.cancel()
	w.closeSource()
	w.started = false
}

// Changed returns a channel that receives an empty struct whenever the Dolt
// database hash changes. The channel has a buffer of 1; rapid consecutive
// changes will be coalesced into a single notification.
func (w *DoltWatcher) Changed() <-chan struct{} {
	return w.changeCh
}

// SetOnChange registers a callback to invoke when the database hash changes. The
// callback is called from the poll goroutine and should not block.
func (w *DoltWatcher) SetOnChange(fn func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onChange = fn
}

// IsPolling always returns true for DoltWatcher because it uses hash polling
// rather than filesystem events.
func (w *DoltWatcher) IsPolling() bool {
	return true
}

// poll checks the hash once per interval, waking early when the user returns
// from idle. It terminates when the context is cancelled.
func (w *DoltWatcher) poll() {
	timer := time.NewTimer(w.pollInterval)
	defer timer.Stop()

	for {
		interval, wake := w.nextPoll()
		timer.Reset(interval)

		select {
		case <-w.ctx.Done():
			return
		case <-wake:
			// Go 1.23 timers discard a pending tick on Stop and Reset, so
			// no drain is needed here.
			timer.Stop()
		case <-timer.C:
		}
		w.check()
	}
}

// check compares the current working-set content hash to the last known hash.
func (w *DoltWatcher) check() {
	hash, err := w.hash()
	if err != nil {
		// Transient error: the next poll retries.
		return
	}

	w.mu.Lock()
	last := w.lastHash
	changed := last != "" && hash != last
	if hash != last {
		w.lastHash = hash
	}
	w.mu.Unlock()

	if changed {
		w.notifyChange()
	}
}

// notifyChange performs a non-blocking send to changeCh and invokes the
// onChange callback if one has been set.
func (w *DoltWatcher) notifyChange() {
	// Non-blocking send: if the channel already has a pending notification the
	// caller has not yet consumed, we drop the duplicate.
	select {
	case w.changeCh <- struct{}{}:
	default:
	}

	w.mu.RLock()
	fn := w.onChange
	w.mu.RUnlock()

	if fn != nil {
		fn()
	}
}
