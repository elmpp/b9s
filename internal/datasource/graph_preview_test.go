package datasource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/vanderheijden86/b9s/internal/bdrun"
)

func TestParseGraphPreviewKeepsMemoriesAndOwnedLinks(t *testing.T) {
	input := `{"preview":true,"schemaVersion":1,"result":{"items":[{"id":"https://example.test/team/beads/policy","type":"https://example.test/team/types/preview-memory-v2","version":"v2","properties":{"title":"Code flow policy","body":"Use integration."},"owned":[{"id":"https://example.test/team/links/policy-work","type":"https://example.test/team/types/preview-related-v2","source":"https://example.test/team/beads/policy","target":"https://example.test/team/beads/work","properties":{"note":"applies to"}}]},{"id":"https://example.test/team/beads/work","type":"https://example.test/team/types/preview-issue-v2","properties":{"title":"Adopt policy","status":"open"},"owned":[]}],"hasMore":false}}`
	graph, err := ParseGraphPreview(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Beads) != 2 || graph.Beads[0].Kind != "memory" || graph.Beads[0].Properties.Body != "Use integration." {
		t.Fatalf("unexpected beads: %+v", graph.Beads)
	}
	if got := graph.Beads[0].Owned[0].Target; got != "https://example.test/team/beads/work" {
		t.Fatalf("link target = %q", got)
	}
}

func TestParseGraphPreviewRejectsTruncatedInventory(t *testing.T) {
	_, err := ParseGraphPreview(strings.NewReader(`{"preview":true,"result":{"items":[],"hasMore":true}}`))
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected limit error, got %v", err)
	}
}

func TestParseGraphPreviewRejectsOrdinaryBeadsOutput(t *testing.T) {
	_, err := ParseGraphPreview(strings.NewReader(`{"result":{"items":[]}}`))
	if err == nil {
		t.Fatal("expected preview format error")
	}
}

func TestParseGraphPreviewLinksReadsIncomingIssueLink(t *testing.T) {
	input := `{"preview":true,"result":[{"id":"links/work-policy","type":"types/preview-related-v2","source":"beads/work","target":"beads/policy","properties":{"note":"cites policy"}}]}`
	links, err := ParseGraphPreviewLinks(strings.NewReader(input))
	if err != nil || len(links) != 1 || links[0].Source != "beads/work" {
		t.Fatalf("links = %+v, err = %v", links, err)
	}
}

// fakeGraphBd writes a bd script that answers each preview read from a fixture
// and records every argument list in calls.log.
func fakeGraphBd(t *testing.T, script string) (bdPath, logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake bd script needs a POSIX shell")
	}
	dir := t.TempDir()
	logPath = filepath.Join(dir, "calls.log")
	body := "#!/bin/sh\necho \"$*\" >> '" + logPath + "'\n" + script
	bdPath = filepath.Join(dir, "bd")
	if err := os.WriteFile(bdPath, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bdPath, logPath
}

func graphWorkspace(t *testing.T) string {
	t.Helper()
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"graph_mode":"link","graph_ready":true}`
	if err := os.WriteFile(filepath.Join(project, ".beads", "metadata.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	return project
}

func TestGraphPreviewClientSearchesMemorySummaries(t *testing.T) {
	bd, log := fakeGraphBd(t, `echo '{"preview":true,"result":{"items":[{"id":"s/beads/adr-0025","title":"ADR 0025","version":"v1","matchedFields":["title","body"],"excerpt":{"field":"body","text":"embedded Dolt","truncated":true},"details":{"ownedLinkCount":1}}]}}'`)
	client, err := newGraphPreviewClient(graphWorkspace(t), bd, bdrun.MemoryDialectPreviewV2)
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.SearchMemories(context.Background(), "embedded Dolt")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "ADR 0025" || got[0].MatchedFields[1] != "body" || got[0].Excerpt.Text != "embedded Dolt" || got[0].Details.OwnedLinkCount != 1 {
		t.Fatalf("summaries = %+v", got)
	}
	calls, _ := os.ReadFile(log)
	if want := "memories embedded Dolt --all --details --format records-json\n"; string(calls) != want {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

func TestGraphPreviewClientListsAllMemoriesWithoutQuery(t *testing.T) {
	bd, log := fakeGraphBd(t, `echo '{"preview":true,"result":{"items":[]}}'`)
	client, _ := newGraphPreviewClient(graphWorkspace(t), bd, bdrun.MemoryDialectPreviewV2)
	if _, err := client.SearchMemories(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	if want := "memories --all --details --format records-json\n"; string(calls) != want {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

func TestGraphPreviewClientRejectsTruncatedSearch(t *testing.T) {
	bd, _ := fakeGraphBd(t, `echo '{"preview":true,"result":{"items":[],"hasMore":true}}'`)
	client, _ := newGraphPreviewClient(graphWorkspace(t), bd, bdrun.MemoryDialectPreviewV2)
	if _, err := client.SearchMemories(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected limit error, got %v", err)
	}
}

func TestGraphPreviewClientReadsRetainedMemoryVersion(t *testing.T) {
	bd, log := fakeGraphBd(t, `echo '{"preview":true,"result":{"id":"s/beads/adr-0030","type":"s/types/preview-memory-v2","version":"old","properties":{"title":"ADR 0030","body":"first body"},"owned":[{"id":"s/links/seed-1","type":"s/types/example-cites","source":"s/beads/adr-0030","target":"s/beads/adr-0025"}]}}'`)
	client, _ := newGraphPreviewClient(graphWorkspace(t), bd, bdrun.MemoryDialectPreviewV2)
	bead, err := client.Memory(context.Background(), "s/beads/adr-0030", "old")
	if err != nil {
		t.Fatal(err)
	}
	if bead.Version != "old" || bead.Properties.Body != "first body" || len(bead.Owned) != 1 || bead.Kind != "memory" {
		t.Fatalf("bead = %+v", bead)
	}
	calls, _ := os.ReadFile(log)
	if want := "show s/beads/adr-0030 --json --version old\n"; string(calls) != want {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

func TestGraphPreviewClientListsVersionsNewestFirst(t *testing.T) {
	bd, _ := fakeGraphBd(t, `echo '{"preview":true,"result":{"kind":"memory","versions":[{"actor":"a","change_at":"2026-10-02T13:05:22Z","ordinal":2,"version":"new"},{"actor":"a","change_at":"2026-10-02T13:04:05Z","ordinal":1,"version":"old"}]}}'`)
	client, _ := newGraphPreviewClient(graphWorkspace(t), bd, bdrun.MemoryDialectPreviewV2)
	versions, err := client.Versions(context.Background(), "s/beads/adr-0030")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Version != "new" || versions[1].Ordinal != 1 || versions[0].ChangeAt.IsZero() {
		t.Fatalf("versions = %+v", versions)
	}
}

func TestGraphPreviewClientReportsPreviewErrorCode(t *testing.T) {
	bd, _ := fakeGraphBd(t, `echo '{"code":"revision_unknown","message":"graph preview retained version is unknown","retryable":false}'; exit 3`)
	client, _ := newGraphPreviewClient(graphWorkspace(t), bd, bdrun.MemoryDialectPreviewV2)
	_, err := client.Memory(context.Background(), "s/beads/adr-0030", "nope")
	if err == nil || !strings.Contains(err.Error(), "revision_unknown") || !strings.Contains(err.Error(), "retained version is unknown") {
		t.Fatalf("expected preview error code, got %v", err)
	}
}

func TestGraphPreviewClientRefusesOrdinaryWorkspace(t *testing.T) {
	project := t.TempDir()
	os.MkdirAll(filepath.Join(project, ".beads"), 0o755)
	os.WriteFile(filepath.Join(project, ".beads", "metadata.json"), []byte(`{"backend":"dolt"}`), 0o644)
	if _, err := newGraphPreviewClient(project, "/bin/false", bdrun.MemoryDialectPreviewV2); !errors.Is(err, ErrMemoryNotEnabled) {
		t.Fatalf("expected workspace error, got %v", err)
	}
}

func TestGraphPreviewClientNamesAStableBdThatLacksGraphCommands(t *testing.T) {
	bd, _ := fakeGraphBd(t, `echo 'Error: unknown flag: --all' >&2; exit 1`)
	client, _ := newGraphPreviewClient(graphWorkspace(t), bd, bdrun.MemoryDialectPreviewV2)
	_, err := client.SearchMemories(context.Background(), "")
	if !errors.Is(err, ErrBDNotGraphPreview) || !strings.Contains(err.Error(), bd) {
		t.Fatalf("expected ErrBDNotGraphPreview naming %s, got %v", bd, err)
	}
}

func TestMemoryReadFailureCategories(t *testing.T) {
	for _, tt := range []struct {
		err            error
		category, hint string
	}{
		{ErrBDNotFound, "bd missing", "PATH"},
		{ErrMemoryNotEnabled, "Memories not enabled", "workspace"},
		{ErrBDNotGraphPreview, "Memory commands unavailable", "bd"},
		{errors.New("Access denied for user bd_test"), "Access denied", "credentials"},
		{errors.New("dial tcp: connection refused"), "Database unreachable", "connection"},
		{context.DeadlineExceeded, "Read timed out", "retry"},
		{errors.New("schema version mismatch"), "Beads schema mismatch", "compatible"},
		{errors.New("unexpected failure"), "Memory read failed", "details"},
	} {
		t.Run(tt.category, func(t *testing.T) {
			got := ExplainMemoryFailure(tt.err)
			if !strings.Contains(got, tt.category) || !strings.Contains(got, tt.hint) || !strings.Contains(got, tt.err.Error()) {
				t.Fatalf("explanation = %s", got)
			}
		})
	}
}

// probeHelpAnswer makes a fake bd answer the Memory capability probe the way
// the preview build does. Prepend it to a fake's script.
const probeHelpAnswer = `case "$*" in *--help) echo "--format string   Graph Memory summaries: table or records-json"; exit 0 ;; esac
`

// preview3HelpAnswer answers the probe the way Graph-mode Preview 3 does: it
// has link list and no links command.
const preview3HelpAnswer = `case "$*" in "links --help") echo "Error: unknown command \"links\" for \"bd\"" >&2; exit 1 ;; *--help) echo "--format string   Graph Memory summaries: table or records-json"; exit 0 ;; esac
`

// stockHelpAnswer answers the probe the way a released bd does.
const stockHelpAnswer = `case "$*" in versions*|links*) echo "Error: unknown command \"$1\" for \"bd\"" >&2; exit 1 ;; *--help) exit 0 ;; esac
`

// onPath puts a bd script on PATH, with an isolated probe cache, and returns
// the file that logs every call that is not a capability probe.
func onPath(t *testing.T, script string) (logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake bd script needs a POSIX shell")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv(bdrun.BDEnv, "")
	bin := t.TempDir()
	logPath = filepath.Join(bin, "calls.log")
	head := "#!/bin/sh\ncase \"$*\" in *--help) ;; *) echo \"$*\" >> '" + logPath + "' ;; esac\n"
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(head+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	return logPath
}

func TestDetectMemory_ReadyWithPreviewBdAndGraphWorkspace(t *testing.T) {
	onPath(t, probeHelpAnswer)

	got := DetectMemory(context.Background(), graphWorkspace(t))

	if got.State != MemoryReady || got.Err != nil {
		t.Fatalf("DetectMemory = %+v, want ready", got)
	}
}

func TestDetectMemory_StockBdIsUnsupported(t *testing.T) {
	onPath(t, stockHelpAnswer)

	got := DetectMemory(context.Background(), graphWorkspace(t))

	if got.State != MemoryUnsupported || !errors.Is(got.Err, ErrBDNotGraphPreview) {
		t.Fatalf("DetectMemory = %+v, want unsupported with ErrBDNotGraphPreview", got)
	}
}

func TestDetectMemory_MissingBdIsUnsupported(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	got := DetectMemory(context.Background(), graphWorkspace(t))

	if got.State != MemoryUnsupported || !errors.Is(got.Err, ErrBDNotFound) {
		t.Fatalf("DetectMemory = %+v, want unsupported with ErrBDNotFound", got)
	}
}

// An ordinary project is the common case: it must cost no bd call at all.
func TestDetectMemory_OrdinaryWorkspaceIsNotEnabledWithoutRunningBd(t *testing.T) {
	ran := filepath.Join(t.TempDir(), "ran")
	onPath(t, "touch '"+ran+"'\n"+probeHelpAnswer)
	project := t.TempDir()
	os.MkdirAll(filepath.Join(project, ".beads"), 0o755)
	writeFile(t, filepath.Join(project, ".beads", "metadata.json"), `{"backend":"dolt"}`)

	got := DetectMemory(context.Background(), project)

	if got.State != MemoryNotEnabled || !errors.Is(got.Err, ErrMemoryNotEnabled) {
		t.Fatalf("DetectMemory = %+v, want not enabled", got)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Error("bd ran for an ordinary workspace, want no bd call")
	}
}

func TestDetectMemory_NewerGraphSchemaIsNotEnabled(t *testing.T) {
	onPath(t, probeHelpAnswer)
	project := graphWorkspace(t)
	writeFile(t, filepath.Join(project, ".beads", "metadata.json"),
		`{"graph_mode":"link","graph_ready":true,"graph_schema_version":99}`)

	got := DetectMemory(context.Background(), project)

	if got.State != MemoryNotEnabled || !strings.Contains(got.Err.Error(), "graph schema 99") {
		t.Fatalf("DetectMemory = %+v, want not enabled naming graph schema 99", got)
	}
}

func TestOpenGraphPreview_RefusesStockBd(t *testing.T) {
	onPath(t, stockHelpAnswer)

	_, err := OpenGraphPreview(graphWorkspace(t))

	if !errors.Is(err, ErrBDNotGraphPreview) {
		t.Fatalf("OpenGraphPreview err = %v, want ErrBDNotGraphPreview", err)
	}
}

// B9S_MEMORY=off can only take Memory away, and it must do so before bd runs.
func TestDetectMemory_LeverTurnsMemoryOffWithoutRunningBd(t *testing.T) {
	ran := filepath.Join(t.TempDir(), "ran")
	onPath(t, "touch '"+ran+"'\n"+probeHelpAnswer)
	t.Setenv("B9S_MEMORY", "off")

	got := DetectMemory(context.Background(), graphWorkspace(t))

	if got.State != MemoryOff {
		t.Errorf("State = %q, want %q", got.State, MemoryOff)
	}
	if reason, _ := MemoryUnavailableReason(got.Err); !strings.Contains(reason, "B9S_MEMORY=off") {
		t.Errorf("reason = %q, want it to name the lever", reason)
	}
	if _, err := os.Stat(ran); !os.IsNotExist(err) {
		t.Error("bd ran although Memory was turned off")
	}
}

func TestDetectMemory_LeverCannotForceMemoryOn(t *testing.T) {
	onPath(t, stockHelpAnswer)
	t.Setenv("B9S_MEMORY", "on")

	got := DetectMemory(context.Background(), graphWorkspace(t))

	if got.State != MemoryUnsupported {
		t.Errorf("State = %q, want %q: the lever may only subtract", got.State, MemoryUnsupported)
	}
}

// preview3Workspace is a graph workspace with the metadata Preview 3 bd wrote.
func preview3Workspace(t *testing.T) string {
	t.Helper()
	project := graphWorkspace(t)
	writeFile(t, filepath.Join(project, ".beads", "metadata.json"), string(readPreview3(t, "metadata.json")))
	return project
}

func readPreview3(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "preview3", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// preview3Script answers each Preview 3 read with what the real bd printed
// for the fixture workspace in testdata/preview3.
func preview3Script(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "preview3"))
	if err != nil {
		t.Fatal(err)
	}
	return `case "$*" in
"types --json") cat '` + dir + `/types.json' ;;
"graph --all --link-type any --json") cat '` + dir + `/graph-all.json' ;;
memories*) cat '` + dir + `/memories.json' ;;
"show "*" --revision "*) cat '` + dir + `/show-revision.json' ;;
"show "*) cat '` + dir + `/show.json' ;;
"versions "*) cat '` + dir + `/versions.json' ;;
"link list "*) cat '` + dir + `/link-list-r1.json' ;;
*) echo "Error: unknown command \"$1\" for \"bd\"" >&2; exit 1 ;;
esac
`
}

func preview3Client(t *testing.T) (*GraphPreviewClient, string) {
	t.Helper()
	bd, log := fakeGraphBd(t, preview3Script(t))
	client, err := newGraphPreviewClient(preview3Workspace(t), bd, bdrun.MemoryDialectPreviewV3)
	if err != nil {
		t.Fatal(err)
	}
	return client, log
}

func TestDetectMemory_Preview3BdReadsSchema11Workspace(t *testing.T) {
	onPath(t, preview3HelpAnswer)

	got := DetectMemory(context.Background(), preview3Workspace(t))

	if got.State != MemoryReady || got.Dialect != bdrun.MemoryDialectPreviewV3 {
		t.Fatalf("DetectMemory = %+v, want ready with the preview-v3 dialect", got)
	}
}

func TestDetectMemory_PreviewV2BdCannotReadSchema11Workspace(t *testing.T) {
	onPath(t, probeHelpAnswer)

	got := DetectMemory(context.Background(), preview3Workspace(t))

	reason, _ := MemoryUnavailableReason(got.Err)
	if got.State != MemoryUnsupported || !strings.Contains(reason, "Preview 3") {
		t.Fatalf("DetectMemory = %+v (reason %q), want unsupported naming Preview 3", got, reason)
	}
}

func TestDetectMemory_Preview3BdCannotReadPreviewV2Workspace(t *testing.T) {
	onPath(t, preview3HelpAnswer)

	got := DetectMemory(context.Background(), graphWorkspace(t))

	if got.State != MemoryUnsupported || got.Err == nil {
		t.Fatalf("DetectMemory = %+v, want unsupported: a preview-v2 workspace needs a preview-v2 bd", got)
	}
}

func TestPreview3InventoryReadsMemoriesIssuesAndAllLinks(t *testing.T) {
	client, log := preview3Client(t)

	inv, err := client.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	kinds := map[string]string{}
	for _, b := range inv.Beads {
		kinds[b.ID] = b.Kind
	}
	want := map[string]string{
		"projects/demo/capabilities/x":  "memory",
		"projects/demo/requirements/r1": "memory",
		"projects/demo/policies/p1":     "memory",
		"ws-2p8":                        "issue",
		"ws-794":                        "issue",
	}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("bead kinds = %v, want %v", kinds, want)
	}
	if len(inv.Links) != 3 {
		t.Fatalf("links = %+v, want the defines, references and blocks Links", inv.Links)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "graph --all --link-type any --json") {
		t.Fatalf("inventory must read every Link Type, not only Dependencies:\n%s", calls)
	}
}

func TestPreview3InventoryKeepsLocalIDsTypesAndRevision(t *testing.T) {
	client, _ := preview3Client(t)

	inv, err := client.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var x GraphBead
	for _, b := range inv.Beads {
		if b.ID == "projects/demo/capabilities/x" {
			x = b
		}
	}
	if x.Type != "specification" || x.Version != "c75e522bf08fd8d56164d5c239c6e74b" || x.Properties.Title != "Capability X" {
		t.Fatalf("capability = %+v, want local Type, revision as Version and its title", x)
	}
	for _, l := range inv.Links {
		if l.Type == "defines" && (l.Source != "projects/demo/capabilities/x" || l.Target != "projects/demo/requirements/r1") {
			t.Fatalf("defines Link = %+v, want scope-local endpoints", l)
		}
	}
}

func TestPreview3InventoryRefusesAnIncompleteGraph(t *testing.T) {
	graph := strings.Replace(string(readPreview3(t, "graph-all.json")), `"complete":true`, `"complete":false`, 1)
	file := filepath.Join(t.TempDir(), "graph.json")
	writeFile(t, file, graph)
	bd, _ := fakeGraphBd(t, `case "$*" in "types --json") cat '`+mustAbs(t, filepath.Join("testdata", "preview3", "types.json"))+`' ;; *) cat '`+file+`' ;; esac`)
	client, err := newGraphPreviewClient(preview3Workspace(t), bd, bdrun.MemoryDialectPreviewV3)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Inventory(context.Background())

	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("Inventory err = %v, want the CLI limit refusal", err)
	}
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestPreview3SearchMemoriesReadsSummaries(t *testing.T) {
	client, _ := preview3Client(t)

	items, err := client.SearchMemories(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 3 || items[0].ID != "projects/demo/capabilities/x" || items[0].Details.OwnedLinkCount != 1 {
		t.Fatalf("summaries = %+v", items)
	}
}

func TestSearchMemoriesRefusesAnIncompleteResult(t *testing.T) {
	bd, _ := fakeGraphBd(t, `echo '{"preview":true,"result":{"complete":false,"items":[],"next":"c2"}}'`)
	client, err := newGraphPreviewClient(preview3Workspace(t), bd, bdrun.MemoryDialectPreviewV3)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.SearchMemories(context.Background(), "")

	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("SearchMemories err = %v, want the CLI limit refusal", err)
	}
}

func TestPreview3MemoryReadsCurrentRecord(t *testing.T) {
	client, _ := preview3Client(t)

	bead, err := client.Memory(context.Background(), "projects/demo/capabilities/x", "")
	if err != nil {
		t.Fatal(err)
	}

	if bead.ID != "projects/demo/capabilities/x" || bead.Kind != "memory" || bead.Version != "c75e522bf08fd8d56164d5c239c6e74b" {
		t.Fatalf("bead = %+v, want the local id, Memory kind and revision as Version", bead)
	}
	if len(bead.Owned) != 1 || bead.Owned[0].Target != "projects/demo/requirements/r1" || bead.Owned[0].Type != "defines" {
		t.Fatalf("owned = %+v, want the defines Link with local ends", bead.Owned)
	}
}

func TestPreview3MemoryReadsRetainedRevisionByToken(t *testing.T) {
	client, log := preview3Client(t)

	bead, err := client.Memory(context.Background(), "projects/demo/capabilities/x", "b37fe5f13c404e4f2131ad6a848f363b")
	if err != nil {
		t.Fatal(err)
	}

	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "--revision =b37fe5f13c404e4f2131ad6a848f363b") {
		t.Fatalf("retained read must select the token with --revision =TOKEN:\n%s", calls)
	}
	if bead.Version != "b37fe5f13c404e4f2131ad6a848f363b" {
		t.Fatalf("version = %q", bead.Version)
	}
}

func TestPreview3LinksReadsBothDirectionsWithLinkList(t *testing.T) {
	client, log := preview3Client(t)

	links, err := client.Links(context.Background(), "projects/demo/requirements/r1")
	if err != nil {
		t.Fatal(err)
	}

	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "link list projects/demo/requirements/r1 --json") {
		t.Fatalf("incident Links must come from link list:\n%s", calls)
	}
	var in, out bool
	for _, l := range links {
		in = in || l.Target == "projects/demo/requirements/r1"
		out = out || l.Source == "projects/demo/requirements/r1"
	}
	if len(links) != 2 || !in || !out {
		t.Fatalf("links = %+v, want one incoming and one outgoing with local ends", links)
	}
}

func TestPreview3VersionsCarryLocalRevisionAsOrdinal(t *testing.T) {
	client, _ := preview3Client(t)

	versions, err := client.Versions(context.Background(), "projects/demo/capabilities/x")
	if err != nil {
		t.Fatal(err)
	}

	if len(versions) != 2 || versions[0].Ordinal != 2 || versions[1].Version != "b37fe5f13c404e4f2131ad6a848f363b" {
		t.Fatalf("versions = %+v", versions)
	}
}

func TestPreview3GraphNeedsNoTraversalAndKeepsPathIDs(t *testing.T) {
	client, log := preview3Client(t)

	g, err := client.Graph(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := g.Node("projects/demo/requirements/r1"); !ok {
		t.Fatalf("nodes = %+v, want path ids kept whole", g.Nodes)
	}
	if got := g.MemoryLinks("projects/demo/requirements/r1"); len(got) != 2 {
		t.Fatalf("r1 Memory Links = %+v, want defines in and references out", got)
	}
	issues := g.Issues()
	var blocked bool
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			blocked = blocked || (issue.ID == "ws-2p8" && dep.DependsOnID == "ws-794")
		}
	}
	if len(issues) != 2 || !blocked {
		t.Fatalf("issues = %+v, want ws-2p8 blocked by ws-794", issues)
	}
	calls, _ := os.ReadFile(log)
	if strings.Count(string(calls), "graph ") != 1 {
		t.Fatalf("the --all read is complete; want no per-component traversal:\n%s", calls)
	}
}
