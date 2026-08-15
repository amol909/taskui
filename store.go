package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Category struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

type Task struct {
	ID           int64
	Name         string
	Completed    int
	CompletedAt  sql.NullString
	Status       string
	CategoryID   sql.NullInt64
	CategoryName string
	ProjectID    sql.NullInt64
	ProjectName  string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DueAt        sql.NullTime // scanned via a string then time.Parse, same as CreatedAt
	DueHasTime   bool
	Priority     int    // 0 none, 1 low, 2 medium, 3 high
	RecurRule    string // canonical recurrence text ("" = not recurring), see recur.go
}

const (
	PriorityNone = 0
	PriorityLow  = 1
	PriorityMed  = 2
	PriorityHigh = 3
)

type Project struct {
	ID        int64
	RootPath  string
	Name      string
	GitRemote string
	LastSeen  time.Time
	CreatedAt time.Time
}

type Store struct {
	conn *sql.DB

	// now overrides the clock used for time-dependent reads such as due-bucket
	// filtering. nil means time.Now. Tests set it so that bucket assertions do
	// not depend on what time of day the suite happens to run at.
	now func() time.Time
}

// clock returns the store's current time, honouring a test override.
func (s *Store) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// parseStoredTime parses a timestamp read back from SQLite. Normally this
// is exactly the app's own "2006-01-02 15:04:05" local-time string, but for
// columns declared DATE/DATETIME/TIMESTAMP the mattn/go-sqlite3 driver
// auto-parses the raw text into a time.Time internally and hands it to
// database/sql as an RFC 3339 string (in UTC) instead - which time.Parse
// with our own layout then fails on. Either way the wall-clock numbers are
// exactly what was stored (local time); this just makes sure they come
// back attached to the Local location instead of silently failing.
func parseStoredTime(s string) (time.Time, error) {
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
		return t, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.Local), nil
		}
	}
	return time.Time{}, fmt.Errorf("parseStoredTime: unrecognised timestamp %q", s)
}

func dbPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	appDir := filepath.Join(configDir, "taskui")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", err
	}

	return filepath.Join(appDir, "taskui.db"), nil
}

func (s *Store) InitDb() error {
	path, err := dbPath()
	if err != nil {
		return err
	}

	return s.Open(path)
}

func (s *Store) Open(path string) error {
	var err error
	s.conn, err = sql.Open("sqlite3", path)
	if err != nil {
		return err
	}

	s.conn.Exec(`PRAGMA foreign_keys = ON;`)
	if path != ":memory:" {
		s.conn.Exec(`PRAGMA journal_mode = WAL;`)
	}

	return s.migrate()
}

func (s *Store) migrate() error {
	categoriesTable := `CREATE TABLE IF NOT EXISTS categories (
		id INTEGER NOT NULL PRIMARY KEY,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	tasksTable := `CREATE TABLE IF NOT EXISTS tasks (
		id INTEGER NOT NULL PRIMARY KEY,
		name TEXT NOT NULL,
		due_date TEXT,
		completed INTEGER DEFAULT 0,
		status TEXT DEFAULT 'todo',
		category_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE SET NULL
	);`

	projectsTable := `CREATE TABLE IF NOT EXISTS projects (
		id         INTEGER NOT NULL PRIMARY KEY,
		root_path  TEXT NOT NULL UNIQUE,
		name       TEXT NOT NULL,
		git_remote TEXT,
		last_seen  DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	// User-defined saved views (see view_spec.go). Built-in views (Today,
	// Overdue, Next 7 days, Blocked) live in code as builtInViews, not in
	// this table, so they can't be renamed or deleted out from under the
	// "1"-"9" keybindings or the palette's "View: ..." commands - only user
	// views are ever rows here.
	viewsTable := `CREATE TABLE IF NOT EXISTS views (
		id       INTEGER NOT NULL PRIMARY KEY,
		name     TEXT NOT NULL UNIQUE COLLATE NOCASE,
		spec     TEXT NOT NULL,
		position INTEGER NOT NULL DEFAULT 0
	);`

	if _, err := s.conn.Exec(categoriesTable); err != nil {
		return err
	}
	if _, err := s.conn.Exec(tasksTable); err != nil {
		return err
	}

	// projects must exist before the tasks.project_id ALTER below, which
	// references it.
	if _, err := s.conn.Exec(projectsTable); err != nil {
		return err
	}

	if _, err := s.conn.Exec(viewsTable); err != nil {
		return err
	}

	// Add category_id column if it doesn't exist (migration for existing DBs)
	s.conn.Exec(`ALTER TABLE tasks ADD COLUMN category_id INTEGER REFERENCES categories(id) ON DELETE SET NULL`)

	// Add status column if it doesn't exist (migration for existing DBs)
	s.conn.Exec(`ALTER TABLE tasks ADD COLUMN status TEXT DEFAULT 'todo'`)

	// Add completed_at column if it doesn't exist (migration for existing DBs)
	s.conn.Exec(`ALTER TABLE tasks ADD COLUMN completed_at DATETIME`)

	// Add project_id column if it doesn't exist (migration for existing DBs).
	// Existing tasks get project_id = NULL, which is the Inbox.
	s.conn.Exec(`ALTER TABLE tasks ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL`)

	// Backfill completed_at for rows completed before this column existed.
	s.conn.Exec(`UPDATE tasks SET completed_at = updated_at WHERE completed = 1 AND completed_at IS NULL`)

	// Add due_at / due_has_time / priority columns if they don't exist
	// (migration for existing DBs). due_at is stored local time in the
	// project's usual "2006-01-02 15:04:05" format. The old due_date TEXT
	// column above is vestigial - it has only ever been written as "" - and
	// is intentionally left in place (dropping columns breaks older
	// SQLite) but is no longer read or written anywhere.
	s.conn.Exec(`ALTER TABLE tasks ADD COLUMN due_at DATETIME`)
	s.conn.Exec(`ALTER TABLE tasks ADD COLUMN due_has_time INTEGER DEFAULT 0`)
	s.conn.Exec(`ALTER TABLE tasks ADD COLUMN priority INTEGER DEFAULT 0`)

	// Add recur_rule column if it doesn't exist (migration for existing DBs).
	// Empty string / NULL means "not recurring"; a non-empty value is the
	// canonical text a RecurRule round-trips through (see recur.go).
	s.conn.Exec(`ALTER TABLE tasks ADD COLUMN recur_rule TEXT`)

	// Legacy trigger from older versions of this app: CURRENT_TIMESTAMP is
	// UTC, but this app writes local-time strings everywhere else, so on any
	// DB carrying this trigger every UPDATE silently rewrote updated_at into
	// a different timezone than the column's other values. It is also
	// redundant - every write path already sets updated_at explicitly.
	s.conn.Exec(`DROP TRIGGER IF EXISTS update_tasks_updated_at`)

	return nil
}

func (s *Store) Close() error {
	return s.conn.Close()
}

// Category methods

func (s *Store) getAllCategories() ([]Category, error) {
	query := `SELECT id, name, created_at FROM categories ORDER BY name COLLATE NOCASE`
	rows, err := s.conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	categories := []Category{}
	for rows.Next() {
		var cat Category
		var createdAtStr string
		if err := rows.Scan(&cat.ID, &cat.Name, &createdAtStr); err != nil {
			return nil, err
		}
		if cat.CreatedAt, err = parseStoredTime(createdAtStr); err != nil {
			cat.CreatedAt = time.Now()
		}
		categories = append(categories, cat)
	}
	return categories, nil
}

func (s *Store) searchCategories(query string) ([]Category, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return s.getAllCategories()
	}

	sqlQuery := `SELECT id, name, created_at FROM categories WHERE LOWER(name) LIKE ? ORDER BY name COLLATE NOCASE`
	rows, err := s.conn.Query(sqlQuery, "%"+query+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	categories := []Category{}
	for rows.Next() {
		var cat Category
		var createdAtStr string
		if err := rows.Scan(&cat.ID, &cat.Name, &createdAtStr); err != nil {
			return nil, err
		}
		if cat.CreatedAt, err = parseStoredTime(createdAtStr); err != nil {
			cat.CreatedAt = time.Now()
		}
		categories = append(categories, cat)
	}
	return categories, nil
}

func (s *Store) findCategoryByName(name string) (*Category, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}

	query := `SELECT id, name, created_at FROM categories WHERE LOWER(name) = LOWER(?)`
	row := s.conn.QueryRow(query, name)

	var cat Category
	var createdAtStr string
	err := row.Scan(&cat.ID, &cat.Name, &createdAtStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if cat.CreatedAt, err = parseStoredTime(createdAtStr); err != nil {
		cat.CreatedAt = time.Now()
	}
	return &cat, nil
}

func (s *Store) createCategory(name string) (*Category, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}

	// Check if already exists (case-insensitive)
	existing, err := s.findCategoryByName(name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	now := time.Now()
	query := `INSERT INTO categories (name, created_at) VALUES (?, ?)`
	result, err := s.conn.Exec(query, name, now.Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return &Category{ID: id, Name: name, CreatedAt: now}, nil
}

func (s *Store) getOrCreateCategory(name string) (*Category, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}

	cat, err := s.findCategoryByName(name)
	if err != nil {
		return nil, err
	}
	if cat != nil {
		return cat, nil
	}
	return s.createCategory(name)
}

// Task methods

func (s *Store) getAllTasks() ([]Task, error) {
	return s.findTasks(TaskQuery{})
}

func (s *Store) getTasksByCategory(categoryID int64) ([]Task, error) {
	return s.findTasks(TaskQuery{CategoryID: &categoryID})
}

func (s *Store) getUncategorizedTasks() ([]Task, error) {
	return s.findTasks(TaskQuery{Uncategorized: true})
}

func (s *Store) findTasks(q TaskQuery) ([]Task, error) {
	query, args := buildTaskQuery(q)
	rows, err := s.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks, err := s.scanTasks(rows)
	if err != nil {
		return nil, err
	}

	if len(q.DueBuckets) > 0 {
		// buildTaskQuery deliberately omitted LIMIT while DueBuckets was
		// set (see its comment), so the bucket filter here sees every
		// coarsely-matched row before Limit is applied - exactly like
		// dueBucketOf/filterByDueBucket's real clock use elsewhere in
		// store.go (insertTask, updateTask, ...), findTasks reads the real
		// clock directly rather than taking now as a parameter.
		labels := make(map[string]bool, len(q.DueBuckets))
		for _, l := range q.DueBuckets {
			labels[l] = true
		}
		tasks = filterByDueBucket(tasks, s.clock(), labels)
		if q.Limit > 0 && len(tasks) > q.Limit {
			tasks = tasks[:q.Limit]
		}
	}

	return tasks, nil
}

func (s *Store) scanTasks(rows *sql.Rows) ([]Task, error) {
	tasks := []Task{}
	for rows.Next() {
		var task Task
		var createdAtStr, updatedAtStr string
		var dueAtStr sql.NullString
		var dueHasTime int
		var recurRule sql.NullString
		err := rows.Scan(&task.ID, &task.Name, &task.Completed, &task.CompletedAt, &task.Status, &task.CategoryID, &task.CategoryName, &task.ProjectID, &task.ProjectName, &createdAtStr, &updatedAtStr, &dueAtStr, &dueHasTime, &task.Priority, &recurRule)
		if err != nil {
			return nil, err
		}
		task.RecurRule = recurRule.String

		if task.Status == "" {
			task.Status = "todo"
		}

		if task.CreatedAt, err = parseStoredTime(createdAtStr); err != nil {
			task.CreatedAt = time.Now()
		}
		if task.UpdatedAt, err = parseStoredTime(updatedAtStr); err != nil {
			task.UpdatedAt = time.Now()
		}

		if dueAtStr.Valid {
			if parsed, parseErr := parseStoredTime(dueAtStr.String); parseErr == nil {
				task.DueAt = sql.NullTime{Time: parsed, Valid: true}
			}
		}
		task.DueHasTime = dueHasTime != 0

		tasks = append(tasks, task)
	}
	return tasks, nil
}

// insertTask inserts a brand-new task, letting SQLite auto-assign the rowid,
// and sets task.ID from the result. Use for new tasks (ID == 0).
func (s *Store) insertTask(task *Task) error {
	if task.Status == "" {
		task.Status = "todo"
	}

	now := time.Now()
	createdAt := task.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}

	var categoryID interface{}
	if task.CategoryID.Valid {
		categoryID = task.CategoryID.Int64
	} else {
		categoryID = nil
	}

	var projectID interface{}
	if task.ProjectID.Valid {
		projectID = task.ProjectID.Int64
	} else {
		projectID = nil
	}

	var completedAt interface{}
	if task.CompletedAt.Valid {
		completedAt = task.CompletedAt.String
	} else {
		completedAt = nil
	}

	var dueAt interface{}
	if task.DueAt.Valid {
		dueAt = task.DueAt.Time.Format("2006-01-02 15:04:05")
	} else {
		dueAt = nil
	}
	dueHasTime := 0
	if task.DueHasTime {
		dueHasTime = 1
	}

	var recurRule interface{}
	if task.RecurRule != "" {
		recurRule = task.RecurRule
	} else {
		recurRule = nil
	}

	query := `INSERT INTO tasks (name, completed, completed_at, status, category_id, project_id, created_at, updated_at, due_at, due_has_time, priority, recur_rule)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := s.conn.Exec(query, task.Name, task.Completed, completedAt, task.Status, categoryID, projectID, createdAt.Format("2006-01-02 15:04:05"), now.Format("2006-01-02 15:04:05"), dueAt, dueHasTime, task.Priority, recurRule)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}

	task.ID = id
	task.CreatedAt = createdAt
	task.UpdatedAt = now
	return nil
}

// updateTask updates an existing task in place. Returns an error if no row
// with task.ID exists.
func (s *Store) updateTask(task Task) error {
	if task.Status == "" {
		task.Status = "todo"
	}

	now := time.Now()

	var categoryID interface{}
	if task.CategoryID.Valid {
		categoryID = task.CategoryID.Int64
	} else {
		categoryID = nil
	}

	var projectID interface{}
	if task.ProjectID.Valid {
		projectID = task.ProjectID.Int64
	} else {
		projectID = nil
	}

	var completedAt interface{}
	if task.CompletedAt.Valid {
		completedAt = task.CompletedAt.String
	} else {
		completedAt = nil
	}

	var dueAt interface{}
	if task.DueAt.Valid {
		dueAt = task.DueAt.Time.Format("2006-01-02 15:04:05")
	} else {
		dueAt = nil
	}
	dueHasTime := 0
	if task.DueHasTime {
		dueHasTime = 1
	}

	var recurRule interface{}
	if task.RecurRule != "" {
		recurRule = task.RecurRule
	} else {
		recurRule = nil
	}

	query := `UPDATE tasks SET name = ?, completed = ?, completed_at = ?, status = ?, category_id = ?, project_id = ?, updated_at = ?, due_at = ?, due_has_time = ?, priority = ?, recur_rule = ? WHERE id = ?`
	result, err := s.conn.Exec(query, task.Name, task.Completed, completedAt, task.Status, categoryID, projectID, now.Format("2006-01-02 15:04:05"), dueAt, dueHasTime, task.Priority, recurRule, task.ID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("updateTask: no task with id %d", task.ID)
	}
	return nil
}

// restoreTask re-inserts a task with an explicit id. Only for undo-restore
// of a previously deleted task.
func (s *Store) restoreTask(task Task) error {
	if task.Status == "" {
		task.Status = "todo"
	}

	now := time.Now()
	createdAt := task.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}

	var categoryID interface{}
	if task.CategoryID.Valid {
		categoryID = task.CategoryID.Int64
	} else {
		categoryID = nil
	}

	var projectID interface{}
	if task.ProjectID.Valid {
		projectID = task.ProjectID.Int64
	} else {
		projectID = nil
	}

	var completedAt interface{}
	if task.CompletedAt.Valid {
		completedAt = task.CompletedAt.String
	} else {
		completedAt = nil
	}

	var dueAt interface{}
	if task.DueAt.Valid {
		dueAt = task.DueAt.Time.Format("2006-01-02 15:04:05")
	} else {
		dueAt = nil
	}
	dueHasTime := 0
	if task.DueHasTime {
		dueHasTime = 1
	}

	var recurRule interface{}
	if task.RecurRule != "" {
		recurRule = task.RecurRule
	} else {
		recurRule = nil
	}

	query := `INSERT INTO tasks (id, name, completed, completed_at, status, category_id, project_id, created_at, updated_at, due_at, due_has_time, priority, recur_rule)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.conn.Exec(query, task.ID, task.Name, task.Completed, completedAt, task.Status, categoryID, projectID, createdAt.Format("2006-01-02 15:04:05"), now.Format("2006-01-02 15:04:05"), dueAt, dueHasTime, task.Priority, recurRule)
	return err
}

// saveTask dispatches to insertTask or updateTask based on whether task.ID
// is set. Kept as a thin wrapper for existing call sites.
func (s *Store) saveTask(task Task) error {
	if task.ID == 0 {
		return s.insertTask(&task)
	}
	return s.updateTask(task)
}

func (s Store) deleteTask(task Task) error {
	queryStatement := `DELETE FROM tasks WHERE id = ?`
	if _, err := s.conn.Exec(queryStatement, task.ID); err != nil {
		return err
	}
	return nil
}

func (s *Store) updateTaskCompletion(taskID int64, completed int) error {
	now := time.Now()

	var completedAt interface{}
	if completed == 1 {
		completedAt = now.Format("2006-01-02 15:04:05")
	} else {
		completedAt = nil
	}

	updateQuery := `UPDATE tasks SET completed = ?, completed_at = ?, updated_at = ? WHERE id = ?`
	if _, err := s.conn.Exec(updateQuery, completed, completedAt, now.Format("2006-01-02 15:04:05"), taskID); err != nil {
		return err
	}
	return nil
}

// completeTask marks a task complete and, when it recurs, inserts the next
// occurrence. Returns the new task's ID, or 0 when nothing was spawned.
// It is a no-op (returns 0, nil, no writes) when the task is already
// completed - callers on the un-complete (1 -> 0) path must use
// updateTaskCompletion directly instead, which never spawns.
func (s *Store) completeTask(task Task, now time.Time) (int64, error) {
	if task.Completed == 1 {
		return 0, nil
	}

	if err := s.updateTaskCompletion(task.ID, 1); err != nil {
		return 0, err
	}

	if task.RecurRule == "" {
		return 0, nil
	}

	rule, ok := ParseRecur(task.RecurRule)
	if !ok {
		return 0, nil
	}

	// Anchor from the task's own due date, not from now - otherwise
	// completing Monday's standup on Wednesday would silently move the
	// series onto Wednesdays. A task with no due date anchors on now.
	anchor := now
	if task.DueAt.Valid {
		anchor = task.DueAt.Time
	}

	// A series left uncompleted for weeks can compute a "next" occurrence
	// that is itself still in the past; keep rolling forward until it is
	// strictly after now, capped as a guard against a malformed rule.
	next := rule.Next(anchor)
	for i := 0; i < 1000 && !next.After(now); i++ {
		next = rule.Next(next)
	}

	newTask := Task{
		Name:       task.Name,
		CategoryID: task.CategoryID,
		ProjectID:  task.ProjectID,
		Priority:   task.Priority,
		DueHasTime: task.DueHasTime,
		RecurRule:  task.RecurRule,
		Status:     "todo",
		Completed:  0,
		DueAt:      sql.NullTime{Time: next, Valid: true},
	}

	if err := s.insertTask(&newTask); err != nil {
		return 0, err
	}

	return newTask.ID, nil
}

func (s *Store) updateTaskStatus(taskID int64, status string) error {
	now := time.Now()
	updateQuery := `UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`
	if _, err := s.conn.Exec(updateQuery, status, now.Format("2006-01-02 15:04:05"), taskID); err != nil {
		return err
	}
	return nil
}

func (s *Store) updateCategory(id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	query := `UPDATE categories SET name = ? WHERE id = ?`
	_, err := s.conn.Exec(query, name, id)
	return err
}

func (s *Store) deleteCategory(id int64) error {
	query := `DELETE FROM categories WHERE id = ?`
	_, err := s.conn.Exec(query, id)
	return err
}

func (s *Store) getTaskCountByCategory(categoryID int64) (int, error) {
	query := `SELECT COUNT(*) FROM tasks WHERE category_id = ?`
	var count int
	err := s.conn.QueryRow(query, categoryID).Scan(&count)
	return count, err
}

// Project methods

// getOrCreateProject upserts a project by root_path, updating name,
// git_remote and last_seen on an existing row.
func (s *Store) getOrCreateProject(r *ProjectRoot) (*Project, error) {
	now := time.Now().Format("2006-01-02 15:04:05")

	var gitRemote interface{}
	if r.GitRemote != "" {
		gitRemote = r.GitRemote
	}

	query := `INSERT INTO projects (root_path, name, git_remote, last_seen)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(root_path) DO UPDATE SET name = excluded.name, git_remote = excluded.git_remote, last_seen = excluded.last_seen`

	if _, err := s.conn.Exec(query, r.Path, r.Name, gitRemote, now); err != nil {
		return nil, err
	}

	row := s.conn.QueryRow(`SELECT id, root_path, name, git_remote, last_seen, created_at FROM projects WHERE root_path = ?`, r.Path)

	var proj Project
	var gitRemoteStr sql.NullString
	var lastSeenStr sql.NullString
	var createdAtStr string
	if err := row.Scan(&proj.ID, &proj.RootPath, &proj.Name, &gitRemoteStr, &lastSeenStr, &createdAtStr); err != nil {
		return nil, err
	}

	proj.GitRemote = gitRemoteStr.String
	if lastSeenStr.Valid {
		if t, err := parseStoredTime(lastSeenStr.String); err == nil {
			proj.LastSeen = t
		}
	}
	if t, err := parseStoredTime(createdAtStr); err == nil {
		proj.CreatedAt = t
	} else {
		proj.CreatedAt = time.Now()
	}

	return &proj, nil
}

func (s *Store) getAllProjects() ([]Project, error) {
	query := `SELECT id, root_path, name, git_remote, last_seen, created_at FROM projects ORDER BY last_seen DESC`
	rows, err := s.conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := []Project{}
	for rows.Next() {
		var proj Project
		var gitRemoteStr sql.NullString
		var lastSeenStr sql.NullString
		var createdAtStr string
		if err := rows.Scan(&proj.ID, &proj.RootPath, &proj.Name, &gitRemoteStr, &lastSeenStr, &createdAtStr); err != nil {
			return nil, err
		}

		proj.GitRemote = gitRemoteStr.String
		if lastSeenStr.Valid {
			if t, err := parseStoredTime(lastSeenStr.String); err == nil {
				proj.LastSeen = t
			}
		}
		if t, err := parseStoredTime(createdAtStr); err == nil {
			proj.CreatedAt = t
		} else {
			proj.CreatedAt = time.Now()
		}

		projects = append(projects, proj)
	}
	return projects, nil
}

// getProjectTaskCount returns the number of open (incomplete) tasks in a project.
func (s *Store) getProjectTaskCount(projectID int64) (int, error) {
	query := `SELECT COUNT(*) FROM tasks WHERE project_id = ? AND completed = 0`
	var count int
	err := s.conn.QueryRow(query, projectID).Scan(&count)
	return count, err
}

// Saved view methods (see view_spec.go for ViewSpec/SavedView/builtInViews)

// getSavedViews returns built-in views first (Today, Overdue, Next 7 days,
// Blocked - always positions 1-4), then user views from the views table
// ordered by their stored position, with Position reassigned sequentially
// (1..N) over the combined list so it always stays contiguous regardless of
// gaps left by deleted user views.
func (s *Store) getSavedViews() ([]SavedView, error) {
	views := make([]SavedView, 0, len(builtInViews))
	views = append(views, builtInViews...)

	rows, err := s.conn.Query(`SELECT id, name, spec, position FROM views ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var v SavedView
		var specStr string
		if err := rows.Scan(&v.ID, &v.Name, &specStr, &v.Position); err != nil {
			return nil, err
		}
		spec, err := unmarshalViewSpec(specStr)
		if err != nil {
			return nil, err
		}
		v.Spec = spec
		views = append(views, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range views {
		views[i].Position = i + 1
	}

	return views, nil
}

// errBuiltInViewName is wrapped into the error saveView returns when name
// collides (case-insensitively) with one of the four built-in views - those
// are what the "1"-"4" keybindings and the palette's "View: ..." commands
// assume are always at fixed positions, so a user-defined row must never be
// able to shadow one. Centralised here (rather than checked separately by
// cli.go and main.go before calling saveView) so there is exactly one place
// that enforces it; callers use errors.Is to turn it into a friendly
// message, the same pattern as the UNIQUE constraint on a duplicate name.
var errBuiltInViewName = errors.New("collides with a built-in view")

// saveView inserts a new user view. Names are case-insensitively unique
// (COLLATE NOCASE on the table) - saving a duplicate name returns the
// underlying SQLite constraint error rather than silently overwriting.
func (s *Store) saveView(name string, spec ViewSpec) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("saveView: name is required")
	}

	if isBuiltInViewName(name) {
		return fmt.Errorf("saveView(%q): %w", name, errBuiltInViewName)
	}

	specStr, err := marshalViewSpec(spec)
	if err != nil {
		return err
	}

	existing, err := s.getSavedViews()
	if err != nil {
		return err
	}
	position := len(existing) + 1

	_, err = s.conn.Exec(`INSERT INTO views (name, spec, position) VALUES (?, ?, ?)`, name, specStr, position)
	return err
}

// deleteView removes a user view by id. Deleting an id that doesn't exist
// (including any built-in's negative id, which never has a row here) is a
// silent no-op, matching deleteTask/deleteCategory's style.
func (s *Store) deleteView(id int64) error {
	_, err := s.conn.Exec(`DELETE FROM views WHERE id = ?`, id)
	return err
}
