package main_test

import (
	"strings"
	"testing"
	"time"
)

// Ctrl+S is XOFF on a terminal with flow control, so only a real PTY proves
// the key reaches the help overlay. Letters that miss the search input would
// dismiss help instead, and the no-match notice would never render.
func TestHelpOverlaySearchWithCtrlS(t *testing.T) {
	tempDir := t.TempDir()
	writeTreeFixture(t, tempDir, makeFilterFixture(t))

	out, err := runTreeTUI(t, tempDir, 2500, []keyStep{
		kd("?", 500*time.Millisecond),
		kd("\x13", 300*time.Millisecond),
		kd("zzzqqq", 300*time.Millisecond),
	})
	if err != nil {
		t.Fatalf("run tree TUI: %v", err)
	}
	containsAll(t, out, []string{"Keyboard Shortcuts", "No shortcuts match"})
}

func TestHelpOverlayDoesNotAdvertiseRemovedViews(t *testing.T) {
	tempDir := t.TempDir()
	writeTreeFixture(t, tempDir, makeFilterFixture(t))

	out, err := runTreeTUI(t, tempDir, 2500, []keyStep{
		kd("?", 500*time.Millisecond),
		kd("\x13", 300*time.Millisecond),
		kd("insights", 300*time.Millisecond),
	})
	if err != nil {
		t.Fatalf("run tree TUI: %v", err)
	}
	containsAll(t, out, []string{"Keyboard Shortcuts", "No shortcuts match"})
}

func TestTutorialQuickStartShowsCurrentViews(t *testing.T) {
	tempDir := t.TempDir()
	writeTreeFixture(t, tempDir, makeFilterFixture(t))

	out, err := runTreeTUIAtSize(t, tempDir, 3000, []keyStep{
		kd("`", 500*time.Millisecond),
		kd("4", 300*time.Millisecond),
		kd("G", 300*time.Millisecond),
	}, 200, 80)
	if err != nil {
		t.Fatalf("run tree TUI: %v", err)
	}
	containsAll(t, out, []string{"Quick Start", "Flat list / tree", "Search the help overlay"})
	for _, stale := range []string{"Insights panel", "History", "Shortcuts sidebar"} {
		if strings.Contains(string(out), stale) {
			t.Errorf("tutorial advertises removed feature %q", stale)
		}
	}
}
