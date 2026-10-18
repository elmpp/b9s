package datasource

import (
	"fmt"
	"time"

	"github.com/vanderheijden86/b9s/pkg/debug"
	"github.com/vanderheijden86/b9s/pkg/model"
)

// leanChangeOverlap reaches back before the previous read's server time. A
// writer whose clock runs behind the server's stamps rows a little in the
// past, and the merge is idempotent, so reading a row twice costs nothing
// while missing it costs a stale screen until the next full read
// (docs/adr/0062-read-only-what-changed-since-the-last-reload.md).
const leanChangeOverlap = 2 * time.Minute

// maxLeanChanges is the number of changed issues above which an incremental
// read costs about as much as a full one and reads in full instead. A
// variable so a test can lower it.
var maxLeanChanges = 500

// changedIssueQuery finds every issue a write has touched since a time. A
// comment or a dependency does not always move its issue's updated_at, so
// their own timestamps count too. Labels carry no timestamp, and a removed
// dependency leaves no row, so the periodic full read covers those.
const changedIssueQuery = `
	SELECT id FROM issues WHERE updated_at > ?
	UNION SELECT issue_id FROM comments WHERE created_at > ?
	UNION SELECT issue_id FROM dependencies WHERE created_at > ?`

// LeanRead is one reload's answer: a full lean snapshot, or the issues that
// changed since an earlier read and the IDs that left the snapshot.
type LeanRead struct {
	Full    bool
	Issues  []model.Issue
	Removed []string
	// AsOf is the server's UTC time just before the read; the next read
	// passes it back as its start point. Zero when the source has no clock to
	// ask, which makes every read full.
	AsOf time.Time
}

// ReadLean reads what changed since a previous read's AsOf, or everything
// when since is zero, when the change is large, or when the incremental
// read fails.
func (r *DoltReader) ReadLean(since time.Time) (LeanRead, error) {
	var asOf time.Time
	if err := r.db.QueryRow(`SELECT UTC_TIMESTAMP(6)`).Scan(&asOf); err != nil {
		return LeanRead{}, fmt.Errorf("read server time: %w", err)
	}
	asOf = asOf.UTC()
	if !since.IsZero() {
		read, full, err := r.readLeanChanges(since)
		if err == nil && !full {
			read.AsOf = asOf
			return read, nil
		}
		if err != nil {
			debug.Log("dolt: incremental read failed, reading in full: %v", err)
		}
	}
	issues, err := r.LoadLeanIssues()
	return LeanRead{Full: true, Issues: issues, AsOf: asOf}, err
}

// readLeanChanges returns the changed issues, or full=true when there are
// too many to read one by one.
func (r *DoltReader) readLeanChanges(since time.Time) (read LeanRead, full bool, err error) {
	cutoff := since.UTC().Add(-leanChangeOverlap)
	rows, err := r.db.Query(changedIssueQuery, cutoff, cutoff, cutoff)
	if err != nil {
		return LeanRead{}, false, fmt.Errorf("query changed issues: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return LeanRead{}, false, fmt.Errorf("scan changed issue: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return LeanRead{}, false, fmt.Errorf("iterate changed issues: %w", err)
	}
	if len(ids) > maxLeanChanges {
		return LeanRead{}, true, nil
	}
	if len(ids) == 0 {
		return LeanRead{}, false, nil
	}

	issues, err := r.readLeanIssues(ids)
	if err != nil {
		return LeanRead{}, false, err
	}
	found := make(map[string]bool, len(issues))
	for _, issue := range issues {
		found[issue.ID] = true
	}
	// A changed ID the lean read did not return is a tombstone or a deleted
	// row: it leaves the snapshot.
	var removed []string
	for _, id := range ids {
		if !found[id] {
			removed = append(removed, id)
		}
	}
	debug.Log("dolt: incremental RESULT: %d changed, %d removed", len(issues), len(removed))
	return LeanRead{Issues: issues, Removed: removed}, false, nil
}

// MergeLean applies read to prev and returns a new slice; prev is left as it
// was. A full read replaces prev entirely.
func MergeLean(prev []model.Issue, read LeanRead) []model.Issue {
	if read.Full {
		return read.Issues
	}
	drop := make(map[string]bool, len(read.Issues)+len(read.Removed))
	for _, id := range read.Removed {
		drop[id] = true
	}
	for _, issue := range read.Issues {
		drop[issue.ID] = true
	}
	merged := make([]model.Issue, 0, len(prev)+len(read.Issues))
	for _, issue := range prev {
		if !drop[issue.ID] {
			merged = append(merged, issue)
		}
	}
	return append(merged, read.Issues...)
}
