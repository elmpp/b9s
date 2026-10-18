package datasource

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The first page is what the tree shows before any closed issue is needed:
// every issue that is not closed, and every epic. Each of them must arrive
// exactly as the full lean read has it, so the full read that follows only
// adds rows and never changes one already on screen.
func TestDoltIntegration_FirstPageHoldsOpenIssuesAndEpicsAsTheFullReadHasThem(t *testing.T) {
	skipIfNoDoltIntegration(t)
	dbName, addr, cleanup := testDB(t)
	defer cleanup()
	db := openMutationDB(t, dbName, addr)
	defer db.Close()
	for _, stmt := range []string{
		`INSERT INTO issues (id, title, description, status, priority, issue_type, closed_at)
			VALUES ('test-006', 'Done task', 'Closed text', 'closed', 2, 'task', NOW())`,
		`INSERT INTO issues (id, title, description, status, priority, issue_type, closed_at)
			VALUES ('test-007', 'Done epic', 'Epic text', 'closed', 1, 'epic', NOW())`,
		`INSERT INTO labels (issue_id, label) VALUES ('test-006', 'old'), ('test-007', 'kept')`,
		`INSERT INTO comments (id, issue_id, author, text, created_at) VALUES ('cmt-epic', 'test-007', 'eve', 'On the epic', NOW())`,
		`INSERT INTO dependencies (id, issue_id, type, depends_on_issue_id, created_at) VALUES ('dep-closed', 'test-006', 'parent-child', 'test-007', NOW())`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	reader := newTestDoltReader(t, dbName, addr)
	defer reader.Close()
	full, err := reader.LoadLeanIssues()
	if err != nil {
		t.Fatalf("full lean read: %v", err)
	}
	page, err := reader.LoadFirstPage()
	if err != nil {
		t.Fatalf("first page: %v", err)
	}

	want := map[string]bool{}
	byID := map[string]int{}
	for i, issue := range full {
		byID[issue.ID] = i
		if !issue.Abridged {
			want[issue.ID] = true
		}
	}
	var got []string
	for _, issue := range page {
		got = append(got, issue.ID)
		if !want[issue.ID] {
			t.Errorf("first page holds %s (status %s), which the full read abridges", issue.ID, issue.Status)
			continue
		}
		if !reflect.DeepEqual(issue, full[byID[issue.ID]]) {
			t.Errorf("first page %s differs from the full read:\n page %+v\n full %+v", issue.ID, issue, full[byID[issue.ID]])
		}
	}
	sort.Strings(got)
	var wantIDs []string
	for id := range want {
		wantIDs = append(wantIDs, id)
	}
	sort.Strings(wantIDs)
	if strings.Join(got, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("first page = %v, want every issue the full read has whole: %v", got, wantIDs)
	}
	if want["test-006"] || !want["test-007"] {
		t.Fatalf("fixture: the closed task must be abridged and the closed epic whole, got %v", wantIDs)
	}
}

// A first-page open of a Dolt server leaves out closed issues and says so, so
// the caller knows the full read is still owed.
func TestDoltIntegration_FirstPageOpenIsPartial(t *testing.T) {
	skipIfNoDoltIntegration(t)
	dbName, addr, cleanup := testDB(t)
	defer cleanup()
	db := openMutationDB(t, dbName, addr)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO issues (id, title, description, status, priority, issue_type, closed_at)
		VALUES ('test-006', 'Done task', 'Closed text', 'closed', 2, 'task', NOW())`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	source := DataSource{Type: SourceTypeDolt, Path: addr, Database: dbName, User: scratchServerUser()}

	opened, failure := OpenProjectFirstPage(OpenTarget{Name: dbName, Dolt: &source})
	if failure != nil {
		t.Fatalf("OpenProjectFirstPage: %v", failure)
	}
	if !opened.Partial {
		t.Fatal("a first-page open of a Dolt server must report Partial")
	}
	for _, issue := range opened.Issues {
		if issue.ID == "test-006" {
			t.Fatal("the first page holds a closed task")
		}
	}
	if len(opened.Issues) == 0 {
		t.Fatal("the first page holds no open issues")
	}

	whole, failure := OpenProjectLean(OpenTarget{Name: dbName, Dolt: &source})
	if failure != nil || whole.Partial {
		t.Fatalf("OpenProjectLean = Partial %t, %v; want a complete open", whole.Partial, failure)
	}
}
