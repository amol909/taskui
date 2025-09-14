package main

import (
	"database/sql"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Task struct {
	ID        int64
	Name      string
	DueDate   string
	Completed int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Store struct {
	conn *sql.DB
}

func (s *Store) InitDb() error {

	var err error
	s.conn, err = sql.Open("sqlite3", "./taskui.db")
	if err != nil {
		return err
	}

	tableCreateStatement := `CREATE TABLE IF NOT EXISTS tasks (
		id integer not null primary key,
		name text not null,
		due_date text,
		completed integer default 0,
		created_at datetime default CURRENT_TIMESTAMP,
		updated_at datetime default CURRENT_TIMESTAMP
	);`

	_, err = s.conn.Exec(tableCreateStatement)
	if err != nil {
		return nil
	}
	return nil
}

func (s *Store) getAllTasks() ([]Task, error) {

	getAllTasksStatement := `SELECT id, name, due_date, completed, created_at, updated_at 
							FROM tasks 
							WHERE NOT (completed = 1 AND created_at < datetime('now', '-1 day'))
							ORDER BY created_at DESC;`

	rows, err := s.conn.Query(getAllTasksStatement)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := []Task{}

	for rows.Next() {
		var task Task
		var createdAtStr, updatedAtStr string
		err := rows.Scan(&task.ID, &task.Name, &task.DueDate, &task.Completed, &createdAtStr, &updatedAtStr)
		if err != nil {
			return nil, err
		}
		
		// Parse timestamps
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

// creates and also updates based on id
func (s *Store) saveTask(task Task) error {

	if task.ID == 0 {
		task.ID = time.Now().UTC().Unix()
	}
	
	now := time.Now()
	upsertQuery := `INSERT INTO tasks (id,name,due_date,completed,created_at,updated_at)
	VALUES (?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE	SET name=excluded.name, due_date=excluded.due_date, completed=excluded.completed, updated_at=excluded.updated_at;
	`
	if _, err := s.conn.Exec(upsertQuery, task.ID, task.Name, task.DueDate, task.Completed, task.CreatedAt.Format("2006-01-02 15:04:05"), now.Format("2006-01-02 15:04:05")); err != nil {
		return err
	}
	return nil
}

func (s Store) deleteTask(task Task) error {
	queryStatement := `DELETE FROM TASKS WHERE ID = (?)`
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
