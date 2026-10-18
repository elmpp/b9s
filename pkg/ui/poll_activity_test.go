package ui

import (
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vanderheijden86/b9s/internal/datasource"
	"github.com/vanderheijden86/b9s/pkg/config"
	"github.com/vanderheijden86/b9s/pkg/model"
)

type pollClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *pollClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *pollClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newActivityModel returns a model whose activity runs on a fake clock and
// whose config sets the idle interval to idle.
func newActivityModel(t *testing.T, idle time.Duration) (Model, *pollClock) {
	t.Helper()
	clock := &pollClock{now: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
	m := NewModel([]model.Issue{asyncReloadIssue("p-1", "One", clock.now)}, "")
	m.activity = datasource.NewActivity(time.Hour, clock.Now)
	cfg := config.Config{}
	cfg.Refresh.IdlePollInterval = config.RefreshInterval(idle)
	m = m.WithConfig(cfg, "remote", "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return updated.(Model), clock
}

const testActivePoll = 500 * time.Millisecond

func TestConfigSetsTheIdlePollInterval(t *testing.T) {
	m, clock := newActivityModel(t, 9*time.Second)
	clock.Advance(datasource.ActiveWindow)
	if got := m.activity.Interval(testActivePoll); got != 9*time.Second {
		t.Fatalf("idle interval = %s, want the configured 9s", got)
	}
}

func TestKeyPressMakesTheSessionActive(t *testing.T) {
	m, clock := newActivityModel(t, 5*time.Second)
	clock.Advance(datasource.ActiveWindow)
	if got := m.activity.Interval(testActivePoll); got == testActivePoll {
		t.Fatal("the session must be idle before the key")
	}
	m, _ = updateWithin(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, 2*time.Second)
	if got := m.activity.Interval(testActivePoll); got != testActivePoll {
		t.Fatalf("interval after a key = %s, want the active %s", got, testActivePoll)
	}
}

func TestMouseEventMakesTheSessionActive(t *testing.T) {
	m, clock := newActivityModel(t, 5*time.Second)
	clock.Advance(datasource.ActiveWindow)
	m, _ = updateWithin(t, m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}, 2*time.Second)
	if got := m.activity.Interval(testActivePoll); got != testActivePoll {
		t.Fatalf("interval after a mouse event = %s, want the active %s", got, testActivePoll)
	}
}

func TestLosingFocusMakesTheSessionIdle(t *testing.T) {
	m, _ := newActivityModel(t, 5*time.Second)
	m, _ = updateWithin(t, m, tea.BlurMsg{}, 2*time.Second)
	if got := m.activity.Interval(testActivePoll); got != 5*time.Second {
		t.Fatalf("interval without focus = %s, want the idle 5s", got)
	}
	m, _ = updateWithin(t, m, tea.FocusMsg{}, 2*time.Second)
	if got := m.activity.Interval(testActivePoll); got != testActivePoll {
		t.Fatalf("interval after focus returns = %s, want the active %s", got, testActivePoll)
	}
}
