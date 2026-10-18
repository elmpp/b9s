package datasource

import (
	"sync"
	"time"
)

const (
	// ActiveWindow is how long after the last key the user counts as active.
	ActiveWindow = 2 * time.Minute
	// DeepIdleAfter is how long without a key before polling drops to
	// DeepIdlePollInterval.
	DeepIdleAfter = 15 * time.Minute
	// DeepIdlePollInterval is the poll interval of a session nobody has
	// touched for DeepIdleAfter.
	DeepIdlePollInterval = 30 * time.Second
)

// Activity records when the user last used the TUI and whether its terminal
// has focus, and picks a watcher's poll interval from that. Every poll is a
// round trip to the shared Dolt server, so a session left open in a
// background tab should not cost the server what an active one does.
//
// Activity is safe for concurrent use: the UI goroutine writes it and the
// watcher goroutines read it.
type Activity struct {
	mu      sync.Mutex
	now     func() time.Time
	idle    time.Duration
	last    time.Time
	focused bool
	wake    chan struct{}
}

// NewActivity returns an Activity that counts as active from now. idle is the
// interval used between ActiveWindow and DeepIdleAfter.
func NewActivity(idle time.Duration, now func() time.Time) *Activity {
	if now == nil {
		now = time.Now
	}
	return &Activity{now: now, idle: idle, last: now(), focused: true, wake: make(chan struct{})}
}

// SetIdleInterval changes the interval used between ActiveWindow and
// DeepIdleAfter.
func (a *Activity) SetIdleInterval(idle time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.idle = idle
}

// Touch records a key or mouse event. The first one after an idle period
// wakes every watcher, so the data on screen is fresh at once rather than
// after the remainder of a long idle interval.
func (a *Activity) Touch() {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	wasActive := a.activeAt(now)
	a.last = now
	if !wasActive && a.activeAt(now) {
		a.wakeLocked()
	}
}

// SetFocused records whether the terminal has focus. Regaining focus counts
// as activity. Terminals that never report focus leave it true.
func (a *Activity) SetFocused(focused bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	wasActive := a.activeAt(now)
	a.focused = focused
	if focused {
		a.last = now
	}
	if !wasActive && a.activeAt(now) {
		a.wakeLocked()
	}
}

// Woken returns a channel that is closed on the next return from idle.
func (a *Activity) Woken() <-chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.wake
}

// Interval returns the poll interval for a watcher configured with active.
// The result is never shorter than active, so backing off cannot make a
// slowly configured watcher poll faster.
func (a *Activity) Interval(active time.Duration) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if a.activeAt(now) {
		return active
	}
	interval := a.idle
	if now.Sub(a.last) >= DeepIdleAfter {
		interval = max(DeepIdlePollInterval, a.idle)
	}
	return max(interval, active)
}

func (a *Activity) activeAt(now time.Time) bool {
	return a.focused && now.Sub(a.last) < ActiveWindow
}

func (a *Activity) wakeLocked() {
	close(a.wake)
	a.wake = make(chan struct{})
}
