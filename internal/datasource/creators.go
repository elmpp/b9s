package datasource

import (
	"database/sql"

	"github.com/vanderheijden86/b9s/pkg/debug"
	"github.com/vanderheijden86/b9s/pkg/model"
)

// creatorQueries run in order until one succeeds. Older bd schemas lack
// owner, and the oldest lack created_by too; a missing column must never cost
// the rest of the issue data, so creators load apart from the main query.
var creatorQueries = []string{
	`SELECT id, created_by, owner FROM issues WHERE 1 = 1`,
	`SELECT id, created_by, NULL FROM issues WHERE 1 = 1`,
}

// applyCreators fills CreatedBy and Owner on issues from the issues table.
// A schema without either column leaves the issues unchanged.
func applyCreators(db *sql.DB, issues []model.Issue) {
	applyCreatorsWhere(db, issues, "")
}

// issueCreator is one issue's created_by and owner.
type issueCreator struct {
	createdBy string
	owner     string
}

// applyCreatorsWhere reads only the issues rows that match an AND clause such
// as idFilter's.
func applyCreatorsWhere(db *sql.DB, issues []model.Issue, and string, args ...any) {
	if len(issues) == 0 {
		return
	}
	creators := loadCreatorsWhere(db, and, args...)
	for i := range issues {
		if c, ok := creators[issues[i].ID]; ok {
			issues[i].CreatedBy, issues[i].Owner = c.createdBy, c.owner
		}
	}
}

// loadCreatorsWhere reads created_by and owner of the issues rows that match
// an AND clause. A schema without either column gives an empty map.
func loadCreatorsWhere(db *sql.DB, and string, args ...any) map[string]issueCreator {
	result := map[string]issueCreator{}
	for _, query := range creatorQueries {
		rows, err := db.Query(query+and, args...)
		if err != nil {
			debug.Log("creators: %q failed: %v", query, err)
			continue
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var createdBy, owner sql.NullString
			if err := rows.Scan(&id, &createdBy, &owner); err != nil {
				continue
			}
			result[id] = issueCreator{createdBy: createdBy.String, owner: owner.String}
		}
		return result
	}
	return result
}
