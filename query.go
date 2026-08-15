package main

import (
	"strings"
	"time"
)

// TaskQuery describes a filtered set of tasks. Zero value = "all live tasks".
// Later slices extend this struct and buildTaskQuery together — never add a
// field without also handling it in the builder.
type TaskQuery struct {
	CategoryID    *int64     // nil = any category
	Uncategorized bool       // true = only tasks with NULL category_id
	ProjectID     *int64     // nil = do not filter by project
	NoProject     bool       // true = only tasks with NULL project_id (the Inbox)
	Statuses      []string   // empty = any status
	Text          string     // case-insensitive substring match on name
	IncludeDone   bool       // true = include long-completed tasks (skip the 1-day hide rule)
	DueBefore     *time.Time // nil = no upper bound; matches due_at < *DueBefore
	DueAfter      *time.Time // nil = no lower bound; matches due_at >= *DueAfter
	DueSet        *bool      // nil = don't filter; true = due_at IS NOT NULL, false = due_at IS NULL
	// DueBuckets refines results by agenda bucket label after the SQL query
	// runs, because a date-only task's deadline is the END of its day and that
	// cannot be expressed as a fixed SQL bound. Empty = no bucket filter.
	DueBuckets []string
	Limit      int // 0 = no limit
}

// buildTaskQuery renders a TaskQuery to SQL plus its args.
func buildTaskQuery(q TaskQuery) (string, []any) {
	query := `SELECT t.id, t.name, t.completed, t.completed_at, t.status,
	t.category_id, COALESCE(c.name, ''), t.project_id, COALESCE(p.name, ''), t.created_at, t.updated_at,
	t.due_at, t.due_has_time, t.priority, t.recur_rule
	FROM tasks t LEFT JOIN categories c ON t.category_id = c.id
	LEFT JOIN projects p ON t.project_id = p.id`

	var clauses []string
	var args []any

	if q.Uncategorized {
		clauses = append(clauses, "t.category_id IS NULL")
	} else if q.CategoryID != nil {
		clauses = append(clauses, "t.category_id = ?")
		args = append(args, *q.CategoryID)
	}

	if q.NoProject {
		clauses = append(clauses, "t.project_id IS NULL")
	} else if q.ProjectID != nil {
		clauses = append(clauses, "t.project_id = ?")
		args = append(args, *q.ProjectID)
	}

	if len(q.Statuses) > 0 {
		placeholders := make([]string, len(q.Statuses))
		for i, status := range q.Statuses {
			placeholders[i] = "?"
			args = append(args, status)
		}
		clauses = append(clauses, "t.status IN ("+strings.Join(placeholders, ",")+")")
	}

	if q.Text != "" {
		clauses = append(clauses, "LOWER(t.name) LIKE ?")
		args = append(args, "%"+strings.ToLower(q.Text)+"%")
	}

	if q.DueSet != nil {
		if *q.DueSet {
			clauses = append(clauses, "t.due_at IS NOT NULL")
		} else {
			clauses = append(clauses, "t.due_at IS NULL")
		}
	}

	if q.DueAfter != nil {
		clauses = append(clauses, "t.due_at >= ?")
		args = append(args, q.DueAfter.Format("2006-01-02 15:04:05"))
	}

	if q.DueBefore != nil {
		clauses = append(clauses, "t.due_at < ?")
		args = append(args, q.DueBefore.Format("2006-01-02 15:04:05"))
	}

	if !q.IncludeDone {
		clauses = append(clauses, "NOT (t.completed = 1 AND t.completed_at IS NOT NULL AND t.completed_at < datetime('now', 'localtime', '-1 day'))")
	}

	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	query += " ORDER BY t.created_at DESC"

	// When DueBuckets is set, findTasks post-filters by exact agenda bucket
	// after scanning, then applies Limit itself - a SQL LIMIT here would
	// truncate the result set before that filter has run, silently dropping
	// rows that should have survived it.
	if q.Limit > 0 && len(q.DueBuckets) == 0 {
		query += " LIMIT ?"
		args = append(args, q.Limit)
	}

	return query, args
}
