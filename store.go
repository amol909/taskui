package main

import (
	"database/sql"
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
	DueDate      string
	Completed    int
	CategoryID   sql.NullInt64
	CategoryName string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Store struct {
	conn *sql.DB
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

	s.conn, err = sql.Open("sqlite3", path)
	if err != nil {
		return err
	}

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
		category_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE SET NULL
	);`

	if _, err = s.conn.Exec(categoriesTable); err != nil {
		return err
	}
	if _, err = s.conn.Exec(tasksTable); err != nil {
		return err
	}

	// Add category_id column if it doesn't exist (migration for existing DBs)
	s.conn.Exec(`ALTER TABLE tasks ADD COLUMN category_id INTEGER REFERENCES categories(id) ON DELETE SET NULL`)

	return nil
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
		if cat.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAtStr); err != nil {
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
		if cat.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAtStr); err != nil {
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
	if cat.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAtStr); err != nil {
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
	query := `SELECT t.id, t.name, t.due_date, t.completed, t.category_id, COALESCE(c.name, ''), t.created_at, t.updated_at 
			  FROM tasks t
			  LEFT JOIN categories c ON t.category_id = c.id
			  WHERE NOT (t.completed = 1 AND t.created_at < datetime('now', '-1 day'))
			  ORDER BY t.created_at DESC;`

	return s.queryTasks(query)
}

func (s *Store) getTasksByCategory(categoryID int64) ([]Task, error) {
	query := `SELECT t.id, t.name, t.due_date, t.completed, t.category_id, COALESCE(c.name, ''), t.created_at, t.updated_at 
			  FROM tasks t
			  LEFT JOIN categories c ON t.category_id = c.id
			  WHERE t.category_id = ?
			  AND NOT (t.completed = 1 AND t.created_at < datetime('now', '-1 day'))
			  ORDER BY t.created_at DESC;`

	rows, err := s.conn.Query(query, categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanTasks(rows)
}

func (s *Store) getUncategorizedTasks() ([]Task, error) {
	query := `SELECT t.id, t.name, t.due_date, t.completed, t.category_id, '', t.created_at, t.updated_at 
			  FROM tasks t
			  WHERE t.category_id IS NULL
			  AND NOT (t.completed = 1 AND t.created_at < datetime('now', '-1 day'))
			  ORDER BY t.created_at DESC;`

	return s.queryTasks(query)
}

func (s *Store) queryTasks(query string) ([]Task, error) {
	rows, err := s.conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanTasks(rows)
}

func (s *Store) scanTasks(rows *sql.Rows) ([]Task, error) {
	tasks := []Task{}
	for rows.Next() {
		var task Task
		var createdAtStr, updatedAtStr string
		err := rows.Scan(&task.ID, &task.Name, &task.DueDate, &task.Completed, &task.CategoryID, &task.CategoryName, &createdAtStr, &updatedAtStr)
		if err != nil {
			return nil, err
		}

		if task.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAtStr); err != nil {
			task.CreatedAt = time.Now()
		}
		if task.UpdatedAt, err = time.Parse("2006-01-02 15:04:05", updatedAtStr); err != nil {
			task.UpdatedAt = time.Now()
		}

		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (s *Store) saveTask(task Task) error {
	if task.ID == 0 {
		task.ID = time.Now().UTC().Unix()
	}

	now := time.Now()
	upsertQuery := `INSERT INTO tasks (id, name, due_date, completed, category_id, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET name=excluded.name, due_date=excluded.due_date, completed=excluded.completed, category_id=excluded.category_id, updated_at=excluded.updated_at;`

	var categoryID interface{}
	if task.CategoryID.Valid {
		categoryID = task.CategoryID.Int64
	} else {
		categoryID = nil
	}

	if _, err := s.conn.Exec(upsertQuery, task.ID, task.Name, task.DueDate, task.Completed, categoryID, task.CreatedAt.Format("2006-01-02 15:04:05"), now.Format("2006-01-02 15:04:05")); err != nil {
		return err
	}
	return nil
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
	updateQuery := `UPDATE tasks SET completed = ?, updated_at = ? WHERE id = ?`
	if _, err := s.conn.Exec(updateQuery, completed, now.Format("2006-01-02 15:04:05"), taskID); err != nil {
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
