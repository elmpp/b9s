package datasource

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestActivity(idle time.Duration) (*Activity, *fakeClock) {
	clock := &fakeClock{now: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
	return NewActivity(idle, clock.Now), clock
}

func TestActivityIntervalBacksOffWhileIdle(t *testing.T) {
	a, clock := newTestActivity(5 * time.Second)
	active := 500 * time.Millisecond

	steps := []struct {
		advance time.Duration
		want    time.Duration
	}{
		{0, active},
		{ActiveWindow - time.Second, active},
		{time.Second, 5 * time.Second},
		{DeepIdleAfter - ActiveWindow - time.Second, 5 * time.Second},
		{time.Second, DeepIdlePollInterval},
		{time.Hour, DeepIdlePollInterval},
	}
	for i, step := range steps {
		clock.Advance(step.advance)
		if got := a.Interval(active); got != step.want {
			t.Fatalf("step %d: interval = %s, want %s", i, got, step.want)
		}
	}

	a.Touch()
	if got := a.Interval(active); got != active {
		t.Fatalf("interval after a key = %s, want the active %s", got, active)
	}
}

func TestActivityIntervalIsIdleWhileTheTerminalHasNoFocus(t *testing.T) {
	a, _ := newTestActivity(5 * time.Second)
	a.SetFocused(false)
	if got := a.Interval(500 * time.Millisecond); got != 5*time.Second {
		t.Fatalf("interval without focus = %s, want the idle 5s", got)
	}
	a.SetFocused(true)
	if got := a.Interval(500 * time.Millisecond); got != 500*time.Millisecond {
		t.Fatalf("interval after focus returns = %s, want the active 500ms", got)
	}
}

func TestActivityIntervalNeverPollsFasterThanConfigured(t *testing.T) {
	a, clock := newTestActivity(time.Second)
	clock.Advance(ActiveWindow)
	if got := a.Interval(2 * time.Second); got != 2*time.Second {
		t.Fatalf("idle interval below the active one = %s, want the active 2s", got)
	}
	a, clock = newTestActivity(time.Minute)
	clock.Advance(DeepIdleAfter)
	if got := a.Interval(500 * time.Millisecond); got != time.Minute {
		t.Fatalf("deep idle below the idle interval = %s, want the idle 1m", got)
	}
}

func TestActivityWakesOnlyOnTheWayBackFromIdle(t *testing.T) {
	a, clock := newTestActivity(5 * time.Second)

	wake := a.Woken()
	a.Touch()
	select {
	case <-wake:
		t.Fatal("a key while already active must not wake the watchers")
	default:
	}

	clock.Advance(ActiveWindow)
	a.Touch()
	select {
	case <-wake:
	default:
		t.Fatal("the first key after idle must wake the watchers")
	}
	select {
	case <-a.Woken():
		t.Fatal("a new wake channel must start open")
	default:
	}
}

// countingWatcher returns a watcher whose hash function counts its calls and
// reports a change on every call after the baseline.
func countingWatcher(t *testing.T, interval time.Duration) (*DoltWatcher, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	w := newHashWatcher(func() (string, error) {
		n := calls.Add(1)
		return string(rune('a' + n%26)), nil
	}, func() {}, interval)
	return w, &calls
}

func waitFor(t *testing.T, limit time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func TestIdleWatcherPollsAtOnceOnTheFirstKey(t *testing.T) {
	a, clock := newTestActivity(time.Hour)
	clock.Advance(ActiveWindow) // idle: the next poll is an hour away
	w, calls := countingWatcher(t, 10*time.Millisecond)
	w.FollowActivity(a)
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	baseline := calls.Load()
	time.Sleep(100 * time.Millisecond)
	if got := calls.Load(); got != baseline {
		t.Fatalf("an idle watcher polled %d times within 100ms, want none", got-baseline)
	}

	a.Touch()
	if !waitFor(t, 2*time.Second, func() bool { return calls.Load() > baseline }) {
		t.Fatal("the first key after idle did not make the watcher poll")
	}
	select {
	case <-w.Changed():
	case <-time.After(2 * time.Second):
		t.Fatal("the wake poll saw a new hash but sent no change")
	}
}

func TestActiveWatcherPollsAtTheConfiguredInterval(t *testing.T) {
	a, _ := newTestActivity(time.Hour)
	w, calls := countingWatcher(t, 10*time.Millisecond)
	w.FollowActivity(a)
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	if !waitFor(t, 2*time.Second, func() bool { return calls.Load() >= 5 }) {
		t.Fatalf("an active watcher polled %d times in 2s at a 10ms interval", calls.Load())
	}
}

func TestWatcherWithoutActivityKeepsTheFixedInterval(t *testing.T) {
	w, calls := countingWatcher(t, 10*time.Millisecond)
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	if !waitFor(t, 2*time.Second, func() bool { return calls.Load() >= 5 }) {
		t.Fatalf("a watcher without activity polled %d times in 2s at a 10ms interval", calls.Load())
	}
}
