package main

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestBuildTaskQuery(t *testing.T) {
	catID := int64(42)
	projID := int64(7)
	dueBefore := time.Date(2026, 8, 13, 0, 0, 0, 0, time.Local)
	dueAfter := time.Date(2026, 8, 12, 0, 0, 0, 0, time.Local)
	dueSetTrue := true
	dueSetFalse := false

	tests := []struct {
		name           string
		query          TaskQuery
		wantContains   []string
		wantNotContain []string
		wantArgs       []any
	}{
		{
			name:  "zero value",
			query: TaskQuery{},
			wantContains: []string{
				"SELECT t.id, t.name, t.completed, t.completed_at, t.status",
				"FROM tasks t LEFT JOIN categories c ON t.category_id = c.id",
				"NOT (t.completed = 1 AND t.completed_at IS NOT NULL AND t.completed_at < datetime('now', 'localtime', '-1 day'))",
				"ORDER BY t.created_at DESC",
			},
			wantNotContain: []string{"LIMIT", "t.category_id = ?", "t.category_id IS NULL", "t.project_id = ?", "t.project_id IS NULL"},
			wantArgs:       nil,
		},
		{
			name:         "category id set",
			query:        TaskQuery{CategoryID: &catID},
			wantContains: []string{"t.category_id = ?"},
			wantArgs:     []any{int64(42)},
		},
		{
			name:           "uncategorized",
			query:          TaskQuery{Uncategorized: true},
			wantContains:   []string{"t.category_id IS NULL"},
			wantNotContain: []string{"t.category_id = ?"},
			wantArgs:       nil,
		},
		{
			name:         "uncategorized wins over category id",
			query:        TaskQuery{CategoryID: &catID, Uncategorized: true},
			wantContains: []string{"t.category_id IS NULL"},
			wantNotContain: []string{
				"t.category_id = ?",
			},
			wantArgs: nil,
		},
		{
			name:         "multi status",
			query:        TaskQuery{Statuses: []string{"todo", "blocked", "in-progress"}},
			wantContains: []string{"t.status IN (?,?,?)"},
			wantArgs:     []any{"todo", "blocked", "in-progress"},
		},
		{
			name:         "text filter",
			query:        TaskQuery{Text: "Report"},
			wantContains: []string{"LOWER(t.name) LIKE ?"},
			wantArgs:     []any{"%report%"},
		},
		{
			name:         "limit",
			query:        TaskQuery{Limit: 10},
			wantContains: []string{"LIMIT ?"},
			wantArgs:     []any{10},
		},
		{
			name:  "include done skips hide rule",
			query: TaskQuery{IncludeDone: true},
			wantNotContain: []string{
				"NOT (t.completed = 1",
			},
			wantArgs: nil,
		},
		{
			name:         "project id set",
			query:        TaskQuery{ProjectID: &projID},
			wantContains: []string{"t.project_id = ?"},
			wantArgs:     []any{int64(7)},
		},
		{
			name:           "no project",
			query:          TaskQuery{NoProject: true},
			wantContains:   []string{"t.project_id IS NULL"},
			wantNotContain: []string{"t.project_id = ?"},
			wantArgs:       nil,
		},
		{
			name:         "no project wins over project id",
			query:        TaskQuery{ProjectID: &projID, NoProject: true},
			wantContains: []string{"t.project_id IS NULL"},
			wantNotContain: []string{
				"t.project_id = ?",
			},
			wantArgs: nil,
		},
		{
			name:         "due before",
			query:        TaskQuery{DueBefore: &dueBefore},
			wantContains: []string{"t.due_at < ?"},
			wantArgs:     []any{"2026-08-13 00:00:00"},
		},
		{
			name:         "due after",
			query:        TaskQuery{DueAfter: &dueAfter},
			wantContains: []string{"t.due_at >= ?"},
			wantArgs:     []any{"2026-08-12 00:00:00"},
		},
		{
			name:         "due range (after and before together)",
			query:        TaskQuery{DueAfter: &dueAfter, DueBefore: &dueBefore},
			wantContains: []string{"t.due_at >= ?", "t.due_at < ?"},
			wantArgs:     []any{"2026-08-12 00:00:00", "2026-08-13 00:00:00"},
		},
		{
			name:           "due set true",
			query:          TaskQuery{DueSet: &dueSetTrue},
			wantContains:   []string{"t.due_at IS NOT NULL"},
			wantNotContain: []string{"t.due_at IS NULL"},
			wantArgs:       nil,
		},
		{
			name:         "due set false",
			query:        TaskQuery{DueSet: &dueSetFalse},
			wantContains: []string{"t.due_at IS NULL"},
			wantNotContain: []string{
				"t.due_at IS NOT NULL",
			},
			wantArgs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args := buildTaskQuery(tt.query)

			for _, want := range tt.wantContains {
				if !strings.Contains(sql, want) {
					t.Errorf("expected SQL to contain %q, got:\n%s", want, sql)
				}
			}
			for _, notWant := range tt.wantNotContain {
				if strings.Contains(sql, notWant) {
					t.Errorf("expected SQL NOT to contain %q, got:\n%s", notWant, sql)
				}
			}

			if len(args) != len(tt.wantArgs) {
				t.Fatalf("expected %d args, got %d: %v", len(tt.wantArgs), len(args), args)
			}
			for i, want := range tt.wantArgs {
				if args[i] != want {
					t.Errorf("arg[%d]: expected %v, got %v", i, want, args[i])
				}
			}
		})
	}
}

func TestBuildTaskQuery_LimitOnlyAppendedWhenPositive(t *testing.T) {
	sql, args := buildTaskQuery(TaskQuery{Limit: 0})
	if strings.Contains(sql, "LIMIT") {
		t.Errorf("expected no LIMIT clause when Limit is 0, got:\n%s", sql)
	}
	if len(args) != 0 {
		t.Errorf("expected no args when query is otherwise empty, got %v", args)
	}
}

// TestBuildTaskQuery_OmitsLimitWhenDueBucketsSet is the Task 1 fix: LIMIT
// must not be emitted while DueBuckets is set, because findTasks's post-scan
// bucket filter runs after the SQL query - a SQL LIMIT would truncate the
// result set before that filter had a chance to run, dropping rows that
// should have survived it.
func TestBuildTaskQuery_OmitsLimitWhenDueBucketsSet(t *testing.T) {
	sql, args := buildTaskQuery(TaskQuery{Limit: 10, DueBuckets: []string{"Today"}})
	if strings.Contains(sql, "LIMIT") {
		t.Errorf("expected no LIMIT clause when DueBuckets is set, got:\n%s", sql)
	}
	if len(args) != 0 {
		t.Errorf("expected no args (no LIMIT placeholder) when DueBuckets is set, got %v", args)
	}
}

// TestFindTasks_DueBucketsFiltersThenLimits proves findTasks applies
// filterByDueBucket after scanning (excluding a task due earlier today,
// whose deadline has already passed, from the "Today" bucket and including
// it in "Overdue" - the Task 1 correctness bug), and that Limit, suppressed
// in buildTaskQuery while DueBuckets is set, is applied by findTasks itself
// afterwards rather than lost.
func TestFindTasks_DueBucketsFiltersThenLimits(t *testing.T) {
	s := newTestStore(t)

	// Pin the store's clock to midday so the fixture cannot straddle midnight.
	// With real-clock offsets, a "now + 10 minutes" task inserted at 23:54
	// lands in Tomorrow rather than Today and the test fails only at night.
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.Local)
	s.now = func() time.Time { return now }

	future := now.Add(5 * time.Minute)
	past := now.Add(-5 * time.Minute)

	mustInsert := func(name string, due time.Time) {
		t.Helper()
		task := &Task{Name: name, DueAt: sql.NullTime{Time: due, Valid: true}, DueHasTime: true}
		if err := s.insertTask(task); err != nil {
			t.Fatalf("insertTask(%s): %v", name, err)
		}
	}

	mustInsert("today 1", future)
	mustInsert("today 2", future.Add(time.Minute))
	mustInsert("today 3", future.Add(2*time.Minute))
	mustInsert("earlier today", past)

	today, err := s.findTasks(TaskQuery{DueBuckets: []string{"Today"}})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(today) != 3 {
		t.Fatalf("expected 3 tasks in the Today bucket, got %d: %+v", len(today), today)
	}
	for _, task := range today {
		if task.Name == "earlier today" {
			t.Errorf("expected the earlier-today (overdue) task excluded from the Today bucket, got %+v", task)
		}
	}

	overdue, err := s.findTasks(TaskQuery{DueBuckets: []string{"Overdue"}})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(overdue) != 1 || overdue[0].Name != "earlier today" {
		t.Fatalf("expected exactly the earlier-today task in Overdue, got %+v", overdue)
	}

	limited, err := s.findTasks(TaskQuery{DueBuckets: []string{"Today"}, Limit: 1})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("expected Limit=1 to still apply after the bucket filter, got %d tasks", len(limited))
	}
}
