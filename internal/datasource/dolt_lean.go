package datasource

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/vanderheijden86/b9s/pkg/debug"
	"github.com/vanderheijden86/b9s/pkg/model"
)

// leanIssueQuery reads every column the tree, board, filters and sorting need
// and none of the long text columns. On a real project the text of closed
// issues is most of a snapshot's bytes, and nobody reads it until an issue is
// opened (docs/adr/0060-load-a-lean-snapshot-and-text-on-demand.md).
const leanIssueQuery = `
	SELECT
		id, title, status, priority, issue_type,
		assignee, estimated_minutes, created_at, updated_at,
		due_at, closed_at, external_ref, compaction_level,
		compacted_at, compacted_at_commit, original_size,
		source_repo, defer_until
	FROM issues
	WHERE status != 'tombstone'`

const leanIssueOrder = `
	ORDER BY updated_at DESC`

// wholeIssueCondition selects the issues whose text and comments arrive with
// the snapshot: the ones a user is likely to open, and epics, whose
// description the board's lane rail shows.
const wholeIssueCondition = `(i.status NOT IN ('closed', 'tombstone') OR i.issue_type = 'epic')`

const wholeIssueTextQuery = `
	SELECT i.id, i.description, i.design, i.acceptance_criteria, i.notes
	FROM issues i
	WHERE ` + wholeIssueCondition

const wholeIssueCommentsQuery = `
	SELECT c.id, c.issue_id, c.author, c.text, c.created_at
	FROM comments c JOIN issues i ON i.id = c.issue_id
	WHERE ` + wholeIssueCondition

const wholeIssueCommentsOrder = `
	ORDER BY c.created_at`

// IssueText is the part of an issue a lean snapshot leaves out.
type IssueText struct {
	Description        string
	Design             string
	AcceptanceCriteria string
	Notes              string
	Comments           []*model.Comment
}

// ApplyTo fills issue with t and marks it whole.
func (t IssueText) ApplyTo(issue *model.Issue) {
	issue.Description = t.Description
	issue.Design = t.Design
	issue.AcceptanceCriteria = t.AcceptanceCriteria
	issue.Notes = t.Notes
	issue.Comments = t.Comments
	issue.Abridged = false
}

// LoadLeanIssues reads every issue's lean columns, labels and dependencies,
// and the text and comments of issues that are not closed and of epics. The
// other issues come back Abridged; LoadIssueText supplies their text. A schema
// without the lean columns falls back to the full read.
func (r *DoltReader) LoadLeanIssues() ([]model.Issue, error) {
	return r.readLeanIssues(nil)
}

// readLeanIssues reads the lean snapshot of the issues in ids, or of every
// issue when ids is nil. A restricted read never falls back to the full read,
// because that returns every issue and the caller asked for a few.
func (r *DoltReader) readLeanIssues(ids []string) ([]model.Issue, error) {
	inIssues, args := idFilter("id", ids)
	rows, err := r.db.Query(leanIssueQuery+inIssues+leanIssueOrder, args...)
	if err != nil {
		if ids == nil && doltMissingColumnError(err) {
			debug.Log("dolt: lean query FAILED (%v), falling back to the full read", err)
			return r.LoadIssuesFiltered(nil)
		}
		return nil, fmt.Errorf("query issues: %w", err)
	}
	var issues []model.Issue
	for rows.Next() {
		issue, err := scanLeanIssue(rows)
		if err != nil {
			debug.Log("dolt: lean scan error on row: %v", err)
			continue
		}
		issues = append(issues, issue)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating issues: %w", err)
	}

	texts, err := r.loadWholeIssueText(ids)
	if err != nil {
		return nil, err
	}
	inLabels, labelArgs := idFilter("issue_id", ids)
	allLabels := r.labelsWhere(inLabels, labelArgs...)
	allDeps, err := r.dependenciesWhere(inLabels, labelArgs...)
	if err != nil {
		return nil, err
	}
	inComments, commentArgs := idFilter("c.issue_id", ids)
	var comments map[string][]*model.Comment
	commentRows, commentsErr := r.db.Query(wholeIssueCommentsQuery+inComments+wholeIssueCommentsOrder, commentArgs...)
	if commentsErr == nil {
		comments, commentsErr = scanAllComments(commentRows)
	} else {
		commentsErr = fmt.Errorf("%w: query comments: %w", ErrCommentsUnavailable, commentsErr)
	}
	if commentsErr != nil {
		debug.Log("dolt: comments unavailable, issues will report no comments: %v", commentsErr)
	}
	applyCreatorsWhere(r.db, issues, inIssues, args...)

	for i := range issues {
		issues[i].Labels = allLabels[issues[i].ID]
		issues[i].Dependencies = allDeps[issues[i].ID]
		text, whole := texts[issues[i].ID]
		if !whole {
			issues[i].Abridged = true
			continue
		}
		text.Comments = comments[issues[i].ID]
		text.ApplyTo(&issues[i])
	}
	debug.Log("dolt: lean RESULT: %d issues, %d whole", len(issues), len(texts))
	return issues, commentsErr
}

// idFilter returns an AND clause that restricts column to ids, with its
// arguments, or nothing when ids is nil.
func idFilter(column string, ids []string) (string, []any) {
	if ids == nil {
		return "", nil
	}
	if len(ids) == 0 {
		return " AND 1 = 0", nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return " AND " + column + " IN (?" + strings.Repeat(", ?", len(ids)-1) + ")", args
}

func (r *DoltReader) loadWholeIssueText(ids []string) (map[string]IssueText, error) {
	in, args := idFilter("i.id", ids)
	rows, err := r.db.Query(wholeIssueTextQuery+in, args...)
	if err != nil {
		return nil, fmt.Errorf("query issue text: %w", err)
	}
	defer rows.Close()
	texts := map[string]IssueText{}
	for rows.Next() {
		var id string
		var text IssueText
		var description, design, acceptance, notes sql.NullString
		if err := rows.Scan(&id, &description, &design, &acceptance, &notes); err != nil {
			return nil, fmt.Errorf("scan issue text: %w", err)
		}
		text.Description, text.Design = description.String, design.String
		text.AcceptanceCriteria, text.Notes = acceptance.String, notes.String
		texts[id] = text
	}
	return texts, rows.Err()
}

// LoadIssueText reads the text columns and comments of one issue.
func (r *DoltReader) LoadIssueText(id string) (IssueText, error) {
	var text IssueText
	var description, design, acceptance, notes sql.NullString
	err := r.db.QueryRow(
		`SELECT description, design, acceptance_criteria, notes FROM issues WHERE id = ?`, id,
	).Scan(&description, &design, &acceptance, &notes)
	if err != nil {
		return IssueText{}, fmt.Errorf("read text of %s: %w", id, err)
	}
	text.Description, text.Design = description.String, design.String
	text.AcceptanceCriteria, text.Notes = acceptance.String, notes.String

	rows, err := r.db.Query(
		`SELECT id, issue_id, author, text, created_at FROM comments WHERE issue_id = ? ORDER BY created_at`, id)
	if err != nil {
		return IssueText{}, fmt.Errorf("%w: read comments of %s: %w", ErrCommentsUnavailable, id, err)
	}
	comments, err := scanAllComments(rows)
	if err != nil {
		return IssueText{}, err
	}
	text.Comments = comments[id]
	return text, nil
}

func scanLeanIssue(rows *sql.Rows) (model.Issue, error) {
	var issue model.Issue
	var estimatedMinutes, compactionLevel, originalSize sql.NullInt64
	var createdAt, updatedAt, dueAt, closedAt, compactedAt, deferUntil sql.NullTime
	var assignee, externalRef, sourceRepo, compactedAtCommit sql.NullString
	var issueType string
	err := rows.Scan(
		&issue.ID, &issue.Title, &issue.Status, &issue.Priority, &issueType,
		&assignee, &estimatedMinutes, &createdAt, &updatedAt,
		&dueAt, &closedAt, &externalRef, &compactionLevel,
		&compactedAt, &compactedAtCommit, &originalSize,
		&sourceRepo, &deferUntil,
	)
	if err != nil {
		return model.Issue{}, err
	}
	issue.IssueType = model.IssueType(issueType)
	issue.Assignee = assignee.String
	issue.SourceRepo = sourceRepo.String
	if estimatedMinutes.Valid {
		v := int(estimatedMinutes.Int64)
		issue.EstimatedMinutes = &v
	}
	issue.CreatedAt = createdAt.Time
	issue.UpdatedAt = updatedAt.Time
	issue.DueDate = nullTimePtr(dueAt)
	issue.ClosedAt = nullTimePtr(closedAt)
	issue.DeferUntil = nullTimePtr(deferUntil)
	issue.CompactedAt = nullTimePtr(compactedAt)
	if externalRef.Valid {
		s := externalRef.String
		issue.ExternalRef = &s
	}
	if compactedAtCommit.Valid {
		s := compactedAtCommit.String
		issue.CompactedAtCommit = &s
	}
	issue.CompactionLevel = int(compactionLevel.Int64)
	issue.OriginalSize = int(originalSize.Int64)
	return issue, nil
}

func nullTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}
