package datasource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vanderheijden86/b9s/internal/bdrun"
	"github.com/vanderheijden86/b9s/pkg/debug"
)

// ErrBDNotGraphPreview identifies missing commands, independent of release names.
var ErrBDNotGraphPreview = errors.New("this bd has no compatible Memory Beads graph commands; use a bd installation with Memory support on PATH or in " + bdrun.BDEnv)

var ErrMemoryNotEnabled = errors.New("not a ready Memory Beads graph workspace")

// ExplainMemoryFailure uses explicit causes first, then recognizable CLI diagnostics.
// The original diagnostic remains available when the evidence is inconclusive.
func ExplainMemoryFailure(err error) string {
	if err == nil {
		return ""
	}
	category, hint := "Memory read failed", "Check the details below and retry."
	detail := strings.ToLower(err.Error())
	var syntax *json.SyntaxError
	switch {
	case errors.Is(err, ErrBDNotFound):
		category, hint = "bd missing", "Put the project's bd installation on PATH, or name it in "+bdrun.BDEnv+"."
	case errors.Is(err, ErrMemoryNotEnabled), errors.Is(err, os.ErrNotExist):
		category, hint = "Memories not enabled", "This project needs a ready Memory graph workspace."
	case errors.Is(err, ErrBDNotGraphPreview), strings.Contains(detail, "unknown command"), strings.Contains(detail, "unknown flag"):
		category, hint = "Memory commands unavailable", "Use the project's bd installation with Memory support."
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, bdrun.ErrTimeout):
		category, hint = "Read timed out", "Check the database connection and retry."
	case errors.Is(err, os.ErrPermission), strings.Contains(detail, "access denied"), strings.Contains(detail, "permission denied"), strings.Contains(detail, "authentication failed"):
		category, hint = "Access denied", "Check this project's database credentials and read permissions."
	case strings.Contains(detail, "schema version mismatch"):
		category, hint = "Beads schema mismatch", "Use a bd binary compatible with this project's database schema."
	case strings.Contains(detail, "connection refused"), strings.Contains(detail, "unreachable"), strings.Contains(detail, "no such host"):
		category, hint = "Database unreachable", "Check the project's database connection or tunnel."
	case errors.As(err, &syntax), strings.Contains(detail, "did not return graph preview"), strings.Contains(detail, "exceeds the CLI limit"):
		category, hint = "Unsupported Memory response", "Check that bd returns a complete, compatible Memory graph."
	}
	return category + ": " + hint + "\nDetails: " + err.Error()
}

// GraphPreview is the current, bounded Bead inventory returned by the preview CLI.
type GraphPreview struct {
	Beads []GraphBead
	Links []GraphLink
	// Scope is the workspace scope URL that Preview 3 ids were made relative
	// to. Those ids are paths that must stay whole; preview-v2 ids are URLs
	// whose last segment names the Bead, so an empty Scope shortens them.
	Scope string
}

// nodeID is the id a MemoryGraph uses for a Bead or Link of this inventory.
func (g GraphPreview) nodeID(id string) string {
	if g.Scope != "" {
		return id
	}
	return shortBeadID(id)
}

type GraphBead struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Version    string          `json:"version"`
	Properties GraphProperties `json:"properties"`
	Owned      []GraphLink     `json:"owned"`
	Kind       string          `json:"-"`
	// rawProperties keeps every property, including Issue fields that
	// GraphProperties does not name.
	rawProperties json.RawMessage
}

type GraphProperties struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

type GraphLink struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Source     string          `json:"source"`
	Target     string          `json:"target"`
	Properties GraphProperties `json:"properties"`
}

// ParseGraphPreview rejects partial snapshots because a missing edge would make
// the displayed graph misleading. The preview CLI exposes no continuation cursor.
func ParseGraphPreview(r io.Reader) (GraphPreview, error) {
	var payload struct {
		Preview bool `json:"preview"`
		Result  struct {
			Items   []GraphBead `json:"items"`
			HasMore bool        `json:"hasMore"`
		} `json:"result"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return GraphPreview{}, fmt.Errorf("parse graph preview: %w", err)
	}
	if !payload.Preview {
		return GraphPreview{}, fmt.Errorf("bd did not return graph preview records")
	}
	if payload.Result.HasMore {
		return GraphPreview{}, fmt.Errorf("graph preview inventory exceeds the CLI limit; no complete graph is available")
	}
	for i := range payload.Result.Items {
		bead := &payload.Result.Items[i]
		bead.Kind = previewV2Kind(bead.Type)
	}
	graph := GraphPreview{Beads: payload.Result.Items}
	for _, bead := range graph.Beads {
		graph.Links = append(graph.Links, bead.Owned...)
	}
	return graph, nil
}

// previewV2Kind reads a Bead's kind from its preview-v2 Type name, the only
// place that dialect records it.
func previewV2Kind(beadType string) string {
	switch {
	case strings.HasSuffix(beadType, "/preview-memory-v2"):
		return "memory"
	case strings.HasSuffix(beadType, "/preview-issue-v2"):
		return "issue"
	}
	return "other"
}

// parsePreview3Graph reads a whole-workspace bd graph --all. The traversal
// is bounded like any other, so an incomplete one is refused rather than
// drawn without the Beads and Links it left out.
func parsePreview3Graph(r io.Reader) (GraphPreview, string, error) {
	var payload struct {
		Preview bool `json:"preview"`
		Result  struct {
			Beads     []GraphBead `json:"beads"`
			Links     []GraphLink `json:"links"`
			Scope     string      `json:"scope"`
			Traversal struct {
				Complete bool `json:"complete"`
			} `json:"traversal"`
		} `json:"result"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return GraphPreview{}, "", fmt.Errorf("parse graph preview: %w", err)
	}
	if !payload.Preview {
		return GraphPreview{}, "", fmt.Errorf("bd did not return graph preview records")
	}
	if !payload.Result.Traversal.Complete {
		return GraphPreview{}, "", fmt.Errorf("graph preview inventory exceeds the CLI limit; no complete graph is available")
	}
	return GraphPreview{Beads: payload.Result.Beads, Links: payload.Result.Links}, payload.Result.Scope, nil
}

func ParseGraphPreviewLinks(r io.Reader) ([]GraphLink, error) {
	var payload struct {
		Preview bool        `json:"preview"`
		Result  []GraphLink `json:"result"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, fmt.Errorf("parse graph preview links: %w", err)
	}
	if !payload.Preview {
		return nil, fmt.Errorf("bd did not return graph preview links")
	}
	return payload.Result, nil
}

// GraphPreviewClient runs read-only preview bd commands against one graph
// workspace. It never opens the preview's Dolt store, which an open embedded
// store would lock against bd itself.
type GraphPreviewClient struct {
	projectDir string
	beadsDir   string
	bd         string
	dialect    bdrun.MemoryDialect
	// scope is the workspace scope URL. Preview 3 prints every id and Type
	// as an absolute URL under it; the client hands out the part after it.
	scope string

	kindsMu sync.Mutex
	// kinds maps each Preview 3 Bead Type to the kind it conforms to.
	kinds map[string]string
	// traversalWorkers bounds concurrent traversals in Graph; zero means
	// defaultTraversalWorkers.
	traversalWorkers int
}

// MemorySummary is one compact result of bd memories. Search is a literal
// title and body match, so a summary carries which fields matched.
type MemorySummary struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Version       string   `json:"version"`
	MatchedFields []string `json:"matchedFields"`
	Excerpt       struct {
		Field     string `json:"field"`
		Text      string `json:"text"`
		Truncated bool   `json:"truncated"`
	} `json:"excerpt"`
	Details struct {
		OwnedLinkCount int `json:"ownedLinkCount"`
	} `json:"details"`
}

// GraphVersion is one retained version of a graph Resource. The token is the
// only stable reference; the ordinal orders versions within this store.
type GraphVersion struct {
	Version  string    `json:"version"`
	Ordinal  int       `json:"ordinal"`
	ChangeAt time.Time `json:"change_at"`
	Actor    string    `json:"actor"`
	Removed  bool      `json:"removed"`
}

// maxGraphSchemaVersion is the newest graph workspace format b9s reads. A newer
// workspace may encode Types or Links this reader would misdraw, so it reads
// as not enabled rather than as a parse failure. b9s reads a graph workspace
// only through bd's JSON, never its tables, so the schema number matters for
// which bd dialect can open it: preview-v2 bd writes schemas up to
// maxPreviewV2GraphSchema, Preview 3 bd writes the ones after it.
const (
	maxGraphSchemaVersion   = 11
	maxPreviewV2GraphSchema = 6
)

// MemoryState says whether Memory views can open for a project.
type MemoryState string

const (
	// MemoryReady: the workspace is a ready graph workspace and the bd on
	// PATH speaks a Memory dialect b9s reads.
	MemoryReady MemoryState = "ready"
	// MemoryUnsupported: the bd on PATH is missing or has no Memory dialect.
	MemoryUnsupported MemoryState = "unsupported"
	// MemoryNotEnabled: the project is not a ready graph workspace.
	MemoryNotEnabled MemoryState = "not_enabled"
	// MemoryOff: the user turned Memory views off with MemoryLeverEnv.
	MemoryOff MemoryState = "off"
)

// MemoryLeverEnv turns Memory views off when set to "off". It can only
// subtract: no value forces Memory on against a failing detection.
const MemoryLeverEnv = "B9S_MEMORY"

// MemoryCapability is the outcome of DetectMemory. Err is nil only when
// State is MemoryReady, and ExplainMemoryFailure turns it into the reason.
type MemoryCapability struct {
	State   MemoryState
	Dialect bdrun.MemoryDialect
	BD      string
	Err     error
}

// MemoryUnavailableError is DetectMemory's refusal. Reason is one line for a
// status bar; the wrapped cause keeps errors.Is and the full diagnostic.
type MemoryUnavailableError struct {
	Reason string
	cause  error
}

func (e *MemoryUnavailableError) Error() string { return e.cause.Error() }
func (e *MemoryUnavailableError) Unwrap() error { return e.cause }

func unavailable(reason string, cause error) error {
	return &MemoryUnavailableError{Reason: reason, cause: cause}
}

// MemoryUnavailableReason returns the one-line reason when err is a
// DetectMemory refusal, and false for any other error, such as a failed read.
func MemoryUnavailableReason(err error) (string, bool) {
	var refusal *MemoryUnavailableError
	if errors.As(err, &refusal) {
		return refusal.Reason, true
	}
	return "", false
}

// DetectMemory decides whether Memory views can open for projectDir. The
// workspace check comes first because it reads one small file: an ordinary
// project, the common case, never runs bd. The bd check reads only help
// output, cached per binary (ADR 0044, docs/plans/2026-10-07-memory-capability-gating.md).
func DetectMemory(ctx context.Context, projectDir string) MemoryCapability {
	capability := detectMemory(ctx, projectDir)
	reason, _ := MemoryUnavailableReason(capability.Err)
	debug.Log("memory: %s state=%s dialect=%s bd=%s reason=%q", projectDir, capability.State, capability.Dialect, capability.BD, reason)
	return capability
}

func detectMemory(ctx context.Context, projectDir string) MemoryCapability {
	if memoryTurnedOff() {
		return MemoryCapability{State: MemoryOff, Err: unavailable("Memory views are turned off ("+MemoryLeverEnv+"=off)", ErrMemoryNotEnabled)}
	}
	return detectGraphReadable(ctx, projectDir)
}

func memoryTurnedOff() bool { return os.Getenv(MemoryLeverEnv) == "off" }

// detectGraphReadable is DetectMemory without the lever. The loader needs it:
// a graph workspace refuses bd export, so its Issues come only from the graph
// read, and turning the Memory views off must not take the Issues with them.
func detectGraphReadable(ctx context.Context, projectDir string) MemoryCapability {
	meta, err := checkGraphWorkspace(projectDir)
	if err != nil {
		reason := "this project is not a Memory graph workspace"
		var schema *graphSchemaError
		if errors.As(err, &schema) {
			reason = fmt.Sprintf("this project uses graph schema %d, newer than this b9s reads", schema.version)
		}
		return MemoryCapability{State: MemoryNotEnabled, Err: unavailable(reason, err)}
	}
	bd, ok := bdrun.Resolve()
	if !ok {
		return MemoryCapability{State: MemoryUnsupported, Err: unavailable("bd is not on PATH and "+bdrun.BDEnv+" is not set", ErrBDNotFound)}
	}
	dialect, _ := bdrun.ProbeMemoryCached(ctx, bd, bdrun.DefaultProbeCacheFile())
	refuse := func(reason string) MemoryCapability {
		return MemoryCapability{State: MemoryUnsupported, Dialect: dialect, BD: bd,
			Err: unavailable(reason, fmt.Errorf("%s: %w", bd, ErrBDNotGraphPreview))}
	}
	preview3 := meta.SchemaVersion > maxPreviewV2GraphSchema
	switch {
	case dialect == bdrun.MemoryDialectPreviewV2 && preview3:
		return refuse(fmt.Sprintf("this project uses graph schema %d, which needs a Preview 3 bd; bd at %s reads Preview 2", meta.SchemaVersion, bd))
	case dialect == bdrun.MemoryDialectPreviewV3 && !preview3:
		return refuse(fmt.Sprintf("this project uses graph schema %d, which needs a Preview 2 bd; bd at %s reads Preview 3", meta.SchemaVersion, bd))
	case dialect != bdrun.MemoryDialectPreviewV2 && dialect != bdrun.MemoryDialectPreviewV3:
		return refuse("bd at " + bd + " has no Memory Beads support")
	}
	return MemoryCapability{State: MemoryReady, Dialect: dialect, BD: bd}
}

// OpenGraphPreview opens a reader for projectDir when DetectMemory finds it
// ready, and returns DetectMemory's reason otherwise.
func OpenGraphPreview(projectDir string) (*GraphPreviewClient, error) {
	capability := DetectMemory(context.Background(), projectDir)
	if capability.Err != nil {
		return nil, capability.Err
	}
	return newGraphPreviewClient(projectDir, capability.BD, capability.Dialect)
}

func newGraphPreviewClient(projectDir, bd string, dialect bdrun.MemoryDialect) (*GraphPreviewClient, error) {
	meta, err := checkGraphWorkspace(projectDir)
	if err != nil {
		return nil, err
	}
	if dialect == bdrun.MemoryDialectPreviewV3 && meta.ScopeURL == "" {
		return nil, fmt.Errorf("%s has no graph_scope_url: %w", projectDir, ErrMemoryNotEnabled)
	}
	return &GraphPreviewClient{projectDir: projectDir, beadsDir: filepath.Join(projectDir, ".beads"), bd: bd,
		dialect: dialect, scope: meta.ScopeURL}, nil
}

// graphMetadata is what .beads/metadata.json says about a graph workspace.
type graphMetadata struct {
	Mode          string `json:"graph_mode"`
	Ready         bool   `json:"graph_ready"`
	SchemaVersion int    `json:"graph_schema_version"`
	// ScopeURL ends in a slash; Preview 3 ids are absolute URLs under it.
	ScopeURL string `json:"graph_scope_url"`
}

// checkGraphWorkspace reports, from .beads/metadata.json alone, whether
// projectDir is a ready graph workspace in a format this reader understands.
func checkGraphWorkspace(projectDir string) (graphMetadata, error) {
	metadata, err := os.ReadFile(filepath.Join(projectDir, ".beads", "metadata.json"))
	if err != nil {
		return graphMetadata{}, fmt.Errorf("read graph workspace metadata: %w", err)
	}
	var meta graphMetadata
	if err := json.Unmarshal(metadata, &meta); err != nil {
		return graphMetadata{}, fmt.Errorf("parse graph workspace metadata: %w", err)
	}
	if meta.Mode != "link" || !meta.Ready {
		return graphMetadata{}, fmt.Errorf("%s is %w", projectDir, ErrMemoryNotEnabled)
	}
	if meta.SchemaVersion > maxGraphSchemaVersion {
		return graphMetadata{}, &graphSchemaError{project: projectDir, version: meta.SchemaVersion}
	}
	return meta, nil
}

type graphSchemaError struct {
	project string
	version int
}

func (e *graphSchemaError) Error() string {
	return fmt.Sprintf("%s uses graph schema %d, newer than %d this b9s reads: %v",
		e.project, e.version, maxGraphSchemaVersion, ErrMemoryNotEnabled)
}

func (e *graphSchemaError) Unwrap() error { return ErrMemoryNotEnabled }

// run returns bd's stdout. A refused preview read prints a JSON error with a
// stable code on stdout and exits non-zero; that code is the useful part of
// the failure, so it leads the returned error.
func (c *GraphPreviewClient) run(ctx context.Context, args ...string) ([]byte, error) {
	out, stderr, err := bdrun.Output(ctx, c.bd, c.projectDir, []string{"BEADS_DIR=" + c.beadsDir}, args...)
	if err == nil {
		return out, nil
	}
	var refusal struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(out, &refusal) == nil && refusal.Code != "" {
		return nil, fmt.Errorf("bd %s: %s: %s", args[0], refusal.Code, refusal.Message)
	}
	if strings.Contains(stderr, "unknown flag") || strings.Contains(stderr, "unknown command") {
		return nil, fmt.Errorf("%s: %w", c.bd, ErrBDNotGraphPreview)
	}
	return nil, fmt.Errorf("bd %s: %w: %s", args[0], err, stderr)
}

// Inventory returns every current Bead with its owned Links. Incoming Links
// owned by other Beads appear only on their owners; use Links for one Bead.
//
// Preview 3 lists only Issues in list, so its inventory is the whole-workspace
// graph read with every Link Type, which carries Memories, Issues and all
// their Links at once. The ids it returns are relative to the scope.
func (c *GraphPreviewClient) Inventory(ctx context.Context) (GraphPreview, error) {
	if c.dialect != bdrun.MemoryDialectPreviewV3 {
		out, err := c.run(ctx, "list", "--format", "records-json", "--all")
		if err != nil {
			return GraphPreview{}, err
		}
		return ParseGraphPreview(bytes.NewReader(out))
	}
	kinds, err := c.typeKinds(ctx)
	if err != nil {
		return GraphPreview{}, err
	}
	out, err := c.run(ctx, "graph", "--all", "--link-type", "any", "--json")
	if err != nil {
		return GraphPreview{}, err
	}
	graph, scope, err := parsePreview3Graph(bytes.NewReader(out))
	if err != nil {
		return GraphPreview{}, err
	}
	if scope != "" && scope != c.scope {
		return GraphPreview{}, fmt.Errorf("bd read scope %s, but %s names %s", scope, c.beadsDir, c.scope)
	}
	for i := range graph.Beads {
		graph.Beads[i].Kind = kindOf(kinds, graph.Beads[i].Type)
		c.localBead(&graph.Beads[i])
	}
	for i := range graph.Links {
		c.localLink(&graph.Links[i])
	}
	graph.Scope = c.scope
	return graph, nil
}

// typeKinds reads the Bead Types once per client. A Type's kind is the root it
// conforms to, through any chain: policy conforms to constraint, which
// conforms to memory.
func (c *GraphPreviewClient) typeKinds(ctx context.Context) (map[string]string, error) {
	c.kindsMu.Lock()
	defer c.kindsMu.Unlock()
	if c.kinds != nil {
		return c.kinds, nil
	}
	out, err := c.run(ctx, "types", "--json")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Preview bool `json:"preview"`
		Result  struct {
			BeadTypes []struct {
				ID         string   `json:"id"`
				ConformsTo []string `json:"conformsTo"`
			} `json:"beadTypes"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("parse graph types: %w", err)
	}
	if !payload.Preview {
		return nil, fmt.Errorf("bd did not return graph types")
	}
	parents := map[string][]string{}
	for _, t := range payload.Result.BeadTypes {
		parents[t.ID] = t.ConformsTo
	}
	roots := map[string]string{c.scope + "memory": "memory", c.scope + "issue": "issue"}
	var resolve func(id string, seen map[string]bool) string
	resolve = func(id string, seen map[string]bool) string {
		if kind, ok := roots[id]; ok {
			return kind
		}
		if seen[id] {
			return "other"
		}
		seen[id] = true
		for _, parent := range parents[id] {
			if kind := resolve(parent, seen); kind != "other" {
				return kind
			}
		}
		return "other"
	}
	c.kinds = map[string]string{}
	for id := range parents {
		c.kinds[id] = resolve(id, map[string]bool{})
	}
	return c.kinds, nil
}

func kindOf(kinds map[string]string, beadType string) string {
	if kind, ok := kinds[beadType]; ok {
		return kind
	}
	return "other"
}

// local strips the workspace scope from a Preview 3 id or Type.
func (c *GraphPreviewClient) local(id string) string {
	if c.dialect != bdrun.MemoryDialectPreviewV3 {
		return id
	}
	return strings.TrimPrefix(id, c.scope)
}

func (c *GraphPreviewClient) localBead(b *GraphBead) {
	b.ID, b.Type = c.local(b.ID), c.local(b.Type)
	for i := range b.Owned {
		c.localLink(&b.Owned[i])
	}
}

func (c *GraphPreviewClient) localLink(l *GraphLink) {
	l.ID, l.Type, l.Source, l.Target = c.local(l.ID), c.local(l.Type), c.local(l.Source), c.local(l.Target)
}

// SearchMemories lists Memory summaries whose title or body contains query
// literally. An empty query lists every Memory.
func (c *GraphPreviewClient) SearchMemories(ctx context.Context, query string) ([]MemorySummary, error) {
	args := []string{"memories"}
	if query != "" {
		args = append(args, query)
	}
	out, err := c.run(ctx, append(args, "--all", "--details", "--format", "records-json")...)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Preview bool `json:"preview"`
		Result  struct {
			Items   []MemorySummary `json:"items"`
			HasMore bool            `json:"hasMore"`
			// Complete is Preview 3's form of the same signal.
			Complete *bool `json:"complete"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("parse graph memory summaries: %w", err)
	}
	if !payload.Preview {
		return nil, fmt.Errorf("bd did not return graph memory summaries")
	}
	if payload.Result.HasMore || (payload.Result.Complete != nil && !*payload.Result.Complete) {
		return nil, fmt.Errorf("graph memory search exceeds the CLI limit; no complete result is available")
	}
	return payload.Result.Items, nil
}

// Memory reads one Bead record: the current version when version is empty,
// otherwise that exact retained version. Owned holds the outgoing Links as
// of the version read.
func (c *GraphPreviewClient) Memory(ctx context.Context, id, version string) (GraphBead, error) {
	preview3 := c.dialect == bdrun.MemoryDialectPreviewV3
	args := []string{"show", id, "--json"}
	switch {
	case version != "" && preview3:
		args = append(args, "--revision", "="+version)
	case version != "":
		args = append(args, "--version", version)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return GraphBead{}, err
	}
	var payload struct {
		Preview bool      `json:"preview"`
		Result  GraphBead `json:"result"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return GraphBead{}, fmt.Errorf("parse graph record: %w", err)
	}
	if !payload.Preview {
		return GraphBead{}, fmt.Errorf("bd did not return a graph record")
	}
	bead := payload.Result
	if !preview3 {
		bead.Kind = previewV2Kind(bead.Type)
		return bead, nil
	}
	kinds, err := c.typeKinds(ctx)
	if err != nil {
		return GraphBead{}, err
	}
	bead.Kind = kindOf(kinds, bead.Type)
	c.localBead(&bead)
	return bead, nil
}

// Links returns the current Links in both directions for one Bead.
func (c *GraphPreviewClient) Links(ctx context.Context, id string) ([]GraphLink, error) {
	args := []string{"links", id, "--json"}
	if c.dialect == bdrun.MemoryDialectPreviewV3 {
		args = []string{"link", "list", id, "--json"}
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	links, err := ParseGraphPreviewLinks(bytes.NewReader(out))
	if err != nil {
		return nil, err
	}
	for i := range links {
		c.localLink(&links[i])
	}
	return links, nil
}

// Versions returns the retained versions of one Resource, newest first.
func (c *GraphPreviewClient) Versions(ctx context.Context, id string) ([]GraphVersion, error) {
	out, err := c.run(ctx, "versions", id, "--json")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Preview bool `json:"preview"`
		Result  struct {
			Versions []struct {
				GraphVersion
				// LocalRevision is Preview 3's name for the ordinal.
				LocalRevision int `json:"local_revision"`
			} `json:"versions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("parse graph versions: %w", err)
	}
	if !payload.Preview {
		return nil, fmt.Errorf("bd did not return graph versions")
	}
	versions := make([]GraphVersion, 0, len(payload.Result.Versions))
	for _, v := range payload.Result.Versions {
		if v.Ordinal == 0 {
			v.Ordinal = v.LocalRevision
		}
		versions = append(versions, v.GraphVersion)
	}
	return versions, nil
}
