package datasource

import (
	"errors"
	"fmt"
	"time"

	"github.com/vanderheijden86/b9s/pkg/debug"
	"github.com/vanderheijden86/b9s/pkg/loader"
	"github.com/vanderheijden86/b9s/pkg/model"
)

// LoadIssues performs smart multi-source detection and loading.
// It discovers all available sources (SQLite, JSONL), validates them, selects
// the freshest valid source, and loads issues from it. SQLite is preferred over
// JSONL when both exist at comparable freshness, since SQLite reflects the most
// recent state (including status changes from br operations).
//
// Falls back to legacy JSONL-only loading via loader.LoadIssues if smart
// detection finds no valid sources.
func LoadIssues(repoPath string) ([]model.Issue, error) {
	beadsDir, err := loader.GetBeadsDir(repoPath)
	if err != nil {
		debug.Log("load: GetBeadsDir(%q) failed: %v", repoPath, err)
		return nil, err
	}
	debug.Log("load: LoadIssues repoPath=%q beadsDir=%q", repoPath, beadsDir)

	issues, smartErr := loadSmart(beadsDir, repoPath)
	if smartErr == nil || errors.Is(smartErr, ErrCommentsUnavailable) {
		// A comments-only failure still means the best source opened and
		// every other column loaded: falling back to JSONL here would trade
		// a live, mostly-complete source for a possibly stale export, over
		// nothing worse than a degraded comments view.
		debug.Log("load: loadSmart returned %d issues (smartErr=%v)", len(issues), smartErr)
		return issues, smartErr
	}

	// Fall back to legacy JSONL-only loading
	debug.Log("load: loadSmart failed (%v), falling back to JSONL", smartErr)
	issues, err = loader.LoadIssues(repoPath)
	debug.Log("load: JSONL fallback returned %d issues, err=%v", len(issues), err)
	return issues, err
}

// LoadIssuesFromDir performs smart source detection within a known beads directory.
// This is useful when the caller already knows the .beads path.
func LoadIssuesFromDir(beadsDir string) ([]model.Issue, error) {
	issues, smartErr := loadSmart(beadsDir, "")
	if smartErr == nil || errors.Is(smartErr, ErrCommentsUnavailable) {
		return issues, smartErr
	}

	// Fall back to JSONL
	jsonlPath, err := loader.FindJSONLPath(beadsDir)
	if err != nil {
		return nil, err
	}
	return loader.LoadIssuesFromFile(jsonlPath)
}

// LoadIssuesFromCanonicalSource reads a checkout from its best source and
// reports that source's error as it is. A JSONL export beside a Dolt checkout
// is a copy, not the project: reading it when the server refuses the
// connection would present a database the user cannot read as reachable.
func LoadIssuesFromCanonicalSource(beadsDir string) ([]model.Issue, error) {
	return loadSmart(beadsDir, "")
}

// LoadLeanIssuesFromCanonicalSource is LoadIssuesFromCanonicalSource with
// LoadLeanFromSource's lean read.
func LoadLeanIssuesFromCanonicalSource(beadsDir string) ([]model.Issue, error) {
	return loadSmartMode(beadsDir, "", true)
}

// LoadLeanIssuesFromDir is LoadIssuesFromDir with LoadLeanFromSource's lean
// read. The JSONL fallback is whole, as every JSONL read is.
func LoadLeanIssuesFromDir(beadsDir string) ([]model.Issue, error) {
	issues, smartErr := loadSmartMode(beadsDir, "", true)
	if smartErr == nil || errors.Is(smartErr, ErrCommentsUnavailable) {
		return issues, smartErr
	}
	jsonlPath, err := loader.FindJSONLPath(beadsDir)
	if err != nil {
		return nil, err
	}
	return loader.LoadIssuesFromFile(jsonlPath)
}

// loadDiscoveryOptions avoids a separate validation pass because LoadFromSource
// opens and queries the selected source immediately afterward. This matters for
// remote Dolt servers, where validation otherwise adds another connection and
// COUNT query to every load.
func loadDiscoveryOptions(beadsDir, repoPath string) DiscoveryOptions {
	return DiscoveryOptions{
		BeadsDir:               beadsDir,
		RepoPath:               repoPath,
		ValidateAfterDiscovery: false,
		IncludeInvalid:         false,
	}
}

// loadSmart discovers sources, selects the best candidate, and loads from it.
// Opening and querying the source is the authoritative validation step.
func loadSmart(beadsDir, repoPath string) ([]model.Issue, error) {
	return loadSmartMode(beadsDir, repoPath, false)
}

func loadSmartMode(beadsDir, repoPath string, lean bool) ([]model.Issue, error) {
	best, err := bestSource(beadsDir, repoPath)
	if err != nil {
		return nil, err
	}
	if lean {
		return LoadLeanFromSource(best)
	}
	return LoadFromSource(best)
}

// LoadIssueTextFromDir is LoadIssueText for the source a lean read of
// beadsDir chose.
func LoadIssueTextFromDir(beadsDir, id string) (IssueText, error) {
	best, err := bestSource(beadsDir, "")
	if err != nil {
		return IssueText{}, err
	}
	return LoadIssueText(best, id)
}

func bestSource(beadsDir, repoPath string) (DataSource, error) {
	sources, err := DiscoverSources(loadDiscoveryOptions(beadsDir, repoPath))
	if err != nil {
		debug.Log("load: DiscoverSources failed: %v", err)
		return DataSource{}, err
	}
	debug.Log("load: discovered %d valid sources", len(sources))
	for i, s := range sources {
		debug.Log("load:   [%d] type=%s path=%s db=%s priority=%d", i, s.Type, s.Path, s.Database, s.Priority)
	}
	if len(sources) == 0 {
		return DataSource{}, fmt.Errorf("no valid sources discovered")
	}

	// Use priority-first selection so Dolt (110) always beats JSONL (50).
	// ModTime-first would let a recently-flushed JSONL file win over Dolt,
	// which is wrong since Dolt is the authoritative source when configured.
	opts := DefaultSelectionOptions()
	opts.PreferFreshest = false
	opts.AllowUnvalidated = true
	best, err := SelectBestSourceWithOptions(sources, opts)
	if err != nil {
		debug.Log("load: SelectBestSource failed: %v", err)
		return DataSource{}, err
	}
	debug.Log("load: selected best source: type=%s path=%s db=%s (priority=%d)", best.Type, best.Path, best.Database, best.Priority)
	return best, nil
}

// LoadLeanFromSource reads a Dolt server source with DoltReader.LoadLeanIssues,
// so closed issues other than epics arrive Abridged, and reads every other
// source whole: a file or a local database is read at disk speed, and only the
// server pays for its text over the network
// (docs/adr/0060-load-a-lean-snapshot-and-text-on-demand.md).
func LoadLeanFromSource(source DataSource) ([]model.Issue, error) {
	if source.Type != SourceTypeDolt {
		return LoadFromSource(source)
	}
	reader, err := NewDoltReader(source)
	if err != nil {
		return nil, fmt.Errorf("failed to open Dolt source %s: %w", source.Path, err)
	}
	defer reader.Close()
	issues, err := reader.LoadLeanIssues()
	debug.Log("load: Dolt lean returned %d issues, err=%v", len(issues), err)
	return issues, err
}

// LoadFirstPageFromSource reads a Dolt server source with
// DoltReader.LoadFirstPage. Every other source is read as LoadLeanFromSource
// reads it, so only a Dolt server can return a partial snapshot.
func LoadFirstPageFromSource(source DataSource) ([]model.Issue, error) {
	if source.Type != SourceTypeDolt {
		return LoadLeanFromSource(source)
	}
	reader, err := NewDoltReader(source)
	if err != nil {
		return nil, fmt.Errorf("failed to open Dolt source %s: %w", source.Path, err)
	}
	defer reader.Close()
	issues, err := reader.LoadFirstPage()
	debug.Log("load: Dolt first page returned %d issues, err=%v", len(issues), err)
	return issues, err
}

// ReadLeanFromSource is LoadLeanFromSource that reads only what changed since
// a previous read's AsOf when since is set. Every source other than a Dolt
// server reads in full and carries no AsOf, so every read of it stays full.
func ReadLeanFromSource(source DataSource, since time.Time) (LeanRead, error) {
	if source.Type != SourceTypeDolt {
		issues, err := LoadFromSource(source)
		return LeanRead{Full: true, Issues: issues}, err
	}
	reader, err := NewDoltReader(source)
	if err != nil {
		return LeanRead{}, fmt.Errorf("failed to open Dolt source %s: %w", source.Path, err)
	}
	defer reader.Close()
	return reader.ReadLean(since)
}

// ReadLeanFromDir is ReadLeanFromSource for the best source in beadsDir, with
// LoadLeanIssuesFromDir's JSONL fallback.
func ReadLeanFromDir(beadsDir string, since time.Time) (LeanRead, error) {
	best, err := bestSource(beadsDir, "")
	if err == nil {
		read, readErr := ReadLeanFromSource(best, since)
		if readErr == nil || errors.Is(readErr, ErrCommentsUnavailable) {
			return read, readErr
		}
	}
	jsonlPath, err := loader.FindJSONLPath(beadsDir)
	if err != nil {
		return LeanRead{}, err
	}
	issues, err := loader.LoadIssuesFromFile(jsonlPath)
	return LeanRead{Full: true, Issues: issues}, err
}

// LoadIssueText reads the text and comments of one issue that a lean read
// left Abridged. Only a Dolt server source abridges issues.
func LoadIssueText(source DataSource, id string) (IssueText, error) {
	if source.Type != SourceTypeDolt {
		return IssueText{}, fmt.Errorf("source %s does not abridge issues", source.Type)
	}
	reader, err := NewDoltReader(source)
	if err != nil {
		return IssueText{}, fmt.Errorf("failed to open Dolt source %s: %w", source.Path, err)
	}
	defer reader.Close()
	return reader.LoadIssueText(id)
}

// LoadFromSource loads issues from a specific DataSource, dispatching to the
// appropriate reader based on source type.
func LoadFromSource(source DataSource) ([]model.Issue, error) {
	debug.Log("load: LoadFromSource type=%s path=%s db=%s", source.Type, source.Path, source.Database)
	switch source.Type {
	case SourceTypeSQLite:
		debug.Log("load: opening SQLite reader: %s", source.Path)
		reader, err := NewSQLiteReader(source)
		if err != nil {
			return nil, fmt.Errorf("failed to open SQLite source %s: %w", source.Path, err)
		}
		defer reader.Close()
		issues, err := reader.LoadIssues()
		debug.Log("load: SQLite returned %d issues, err=%v", len(issues), err)
		return issues, err

	case SourceTypeDolt:
		debug.Log("load: opening Dolt reader: %s/%s", source.Path, source.Database)
		reader, err := NewDoltReader(source)
		if err != nil {
			return nil, fmt.Errorf("failed to open Dolt source %s: %w", source.Path, err)
		}
		defer reader.Close()
		issues, err := reader.LoadIssues()
		debug.Log("load: Dolt returned %d issues, err=%v", len(issues), err)
		return issues, err

	case SourceTypeDoltEmbedded:
		debug.Log("load: reading embedded Dolt through bd export: %s", source.Path)
		issues, err := loadEmbeddedIssues(source)
		debug.Log("load: embedded Dolt returned %d issues, err=%v", len(issues), err)
		return issues, err

	case SourceTypeJSONLLocal, SourceTypeJSONLWorktree:
		debug.Log("load: loading JSONL file: %s", source.Path)
		issues, err := loader.LoadIssuesFromFile(source.Path)
		debug.Log("load: JSONL returned %d issues, err=%v", len(issues), err)
		return issues, err

	default:
		return nil, fmt.Errorf("unknown source type: %s", source.Type)
	}
}
