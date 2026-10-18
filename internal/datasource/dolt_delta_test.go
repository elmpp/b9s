package datasource

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/vanderheijden86/b9s/pkg/model"
)

func TestMergeLeanReplacesAddsAndRemoves(t *testing.T) {
	prev := []model.Issue{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C"},
	}
	read := LeanRead{
		Issues:  []model.Issue{{ID: "b", Title: "B2"}, {ID: "d", Title: "D"}},
		Removed: []string{"c"},
	}
	got := MergeLean(prev, read)
	titles := map[string]string{}
	for _, issue := range got {
		titles[issue.ID] = issue.Title
	}
	want := map[string]string{"a": "A", "b": "B2", "d": "D"}
	if !reflect.DeepEqual(titles, want) {
		t.Fatalf("merged = %v, want %v", titles, want)
	}
	if prev[1].Title != "B" {
		t.Fatal("MergeLean changed the previous snapshot in place")
	}
}

func TestMergeLeanOfAFullReadIsTheRead(t *testing.T) {
	read := LeanRead{Full: true, Issues: []model.Issue{{ID: "x"}}}
	got := MergeLean([]model.Issue{{ID: "a"}}, read)
	if len(got) != 1 || got[0].ID != "x" {
		t.Fatalf("merge of a full read = %v, want only x", got)
	}
}

// normalizeLean sorts what the database returns in no fixed order, so two
// reads of the same rows compare equal.
func normalizeLean(issues []model.Issue) map[string]model.Issue {
	out := make(map[string]model.Issue, len(issues))
	for _, issue := range issues {
		sort.Strings(issue.Labels)
		sort.Slice(issue.Dependencies, func(i, j int) bool {
			return issue.Dependencies[i].DependsOnID < issue.Dependencies[j].DependsOnID
		})
		out[issue.ID] = issue
	}
	return out
}

func TestDoltIntegration_LeanChangesMergeToAFullRead(t *testing.T) {
	skipIfNoDoltIntegration(t)
	dbName, addr, cleanup := testDB(t)
	defer cleanup()
	db := openMutationDB(t, dbName, addr)
	defer db.Close()
	exec := func(stmts ...string) {
		t.Helper()
		for _, stmt := range stmts {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("%s: %v", stmt, err)
			}
		}
	}
	// Everything seeded so far is an hour old, outside the overlap window.
	exec(
		`INSERT INTO issues (id, title, description, status, priority, issue_type, closed_at)
			VALUES ('test-006', 'Old work', 'Closed text', 'closed', 2, 'task', NOW())`,
		`UPDATE issues SET updated_at = UTC_TIMESTAMP() - INTERVAL 1 HOUR`,
		`UPDATE comments SET created_at = UTC_TIMESTAMP() - INTERVAL 1 HOUR`,
		`UPDATE dependencies SET created_at = UTC_TIMESTAMP() - INTERVAL 1 HOUR`,
	)

	reader := newTestDoltReader(t, dbName, addr)
	defer reader.Close()
	first, err := reader.ReadLean(time.Time{})
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if !first.Full || first.AsOf.IsZero() {
		t.Fatalf("a read without a start point must be full and carry the server time: full=%t asOf=%s", first.Full, first.AsOf)
	}

	exec(
		// An edit bumps updated_at.
		`UPDATE issues SET title = 'Renamed', updated_at = UTC_TIMESTAMP() WHERE id = 'test-001'`,
		// A comment that leaves updated_at alone, as some writers do.
		`INSERT INTO comments (id, issue_id, author, text, created_at) VALUES ('cmt-new', 'test-002', 'eve', 'Late remark', UTC_TIMESTAMP())`,
		// A new dependency.
		`INSERT INTO dependencies (id, issue_id, type, depends_on_issue_id, created_at) VALUES ('dep-new', 'test-003', 'blocks', 'test-002', UTC_TIMESTAMP())`,
		// A new issue with a label.
		`INSERT INTO issues (id, title, description, status, priority, issue_type, updated_at) VALUES ('test-007', 'Fresh', 'New text', 'open', 1, 'bug', UTC_TIMESTAMP())`,
		`INSERT INTO labels (issue_id, label) VALUES ('test-007', 'fresh')`,
		// A closed issue reopened, and an open one deleted.
		`UPDATE issues SET status = 'open', closed_at = NULL, updated_at = UTC_TIMESTAMP() WHERE id = 'test-006'`,
		`UPDATE issues SET status = 'tombstone', updated_at = UTC_TIMESTAMP() WHERE id = 'test-005'`,
	)

	delta, err := reader.ReadLean(first.AsOf)
	if err != nil {
		t.Fatalf("delta read: %v", err)
	}
	if delta.Full {
		t.Fatal("a read with a start point must be incremental")
	}
	var changed []string
	for _, issue := range delta.Issues {
		changed = append(changed, issue.ID)
	}
	sort.Strings(changed)
	if want := "test-001,test-002,test-003,test-006,test-007"; strings.Join(changed, ",") != want {
		t.Fatalf("changed = %v, want %s", changed, want)
	}
	if !reflect.DeepEqual(delta.Removed, []string{"test-005"}) {
		t.Fatalf("removed = %v, want [test-005]", delta.Removed)
	}
	if !delta.AsOf.After(first.AsOf) && !delta.AsOf.Equal(first.AsOf) {
		t.Fatalf("delta asOf %s is before the first read's %s", delta.AsOf, first.AsOf)
	}

	merged := normalizeLean(MergeLean(first.Issues, delta))
	fresh, err := reader.ReadLean(time.Time{})
	if err != nil {
		t.Fatalf("fresh read: %v", err)
	}
	want := normalizeLean(fresh.Issues)
	if len(merged) != len(want) {
		t.Fatalf("merged has %d issues, a full read %d", len(merged), len(want))
	}
	for id, w := range want {
		if g := merged[id]; !reflect.DeepEqual(g, w) {
			t.Errorf("issue %s differs after merge:\n got  %+v\n want %+v", id, g, w)
		}
	}
}

func TestDoltIntegration_LargeLeanChangeReadsInFull(t *testing.T) {
	skipIfNoDoltIntegration(t)
	dbName, addr, cleanup := testDB(t)
	defer cleanup()
	reader := newTestDoltReader(t, dbName, addr)
	defer reader.Close()

	limit := maxLeanChanges
	maxLeanChanges = 1
	defer func() { maxLeanChanges = limit }()

	// Every seeded row is new, so a read since an hour ago sees them all.
	read, err := reader.ReadLean(time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !read.Full || len(read.Issues) != 4 {
		t.Fatalf("a change larger than the limit must read in full: full=%t issues=%d", read.Full, len(read.Issues))
	}
}
