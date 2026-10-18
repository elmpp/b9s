package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vanderheijden86/b9s/internal/datasource"
)

func TestConnectStartupSourceFallsBackToTheExport(t *testing.T) {
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(path, []byte(`{"id":"x-1","title":"One","status":"open","issue_type":"task","priority":2}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := connectStartupSource(beadsDir, path, time.Second)
	defer got.stop()
	if got.sourceType != datasource.SourceTypeJSONLLocal || got.watcher != nil || got.info != "jsonl issues.jsonl" {
		t.Fatalf("connectStartupSource = %+v, want the JSONL export without a watcher", got)
	}
}
