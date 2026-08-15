package main

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := &Store{}
	if err := s.Open(":memory:"); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertTask_NoIDCollision(t *testing.T) {
	s := newTestStore(t)

	seen := make(map[int64]bool)
	for i := 0; i < 100; i++ {
		task := &Task{Name: "task"}
		if err := s.insertTask(task); err != nil {
			t.Fatalf("insertTask: %v", err)
		}
		if task.ID == 0 {
			t.Fatalf("expected non-zero ID after insertTask")
		}
		if seen[task.ID] {
			t.Fatalf("duplicate task ID %d", task.ID)
		}
		seen[task.ID] = true
	}

	tasks, err := s.findTasks(TaskQuery{})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(tasks) != 100 {
		t.Fatalf("expected 100 tasks, got %d", len(tasks))
	}
}

func TestSaveTask_DispatchesToInsertAndUpdate(t *testing.T) {
	s := newTestStore(t)

	task := Task{Name: "first"}
	if err := s.saveTask(task); err != nil {
		t.Fatalf("saveTask (insert): %v", err)
	}

	tasks, err := s.findTasks(TaskQuery{})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	existing := tasks[0]
	existing.Name = "renamed"
	if err := s.saveTask(existing); err != nil {
		t.Fatalf("saveTask (update): %v", err)
	}

	tasks, err = s.findTasks(TaskQuery{})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task after update, got %d", len(tasks))
	}
	if tasks[0].Name != "renamed" {
		t.Fatalf("expected task name %q, got %q", "renamed", tasks[0].Name)
	}
}

func TestUpdateTask_MissingRowErrors(t *testing.T) {
	s := newTestStore(t)

	err := s.updateTask(Task{ID: 999, Name: "ghost"})
	if err == nil {
		t.Fatalf("expected error updating a task that doesn't exist")
	}
}

func TestUndoRestore(t *testing.T) {
	s := newTestStore(t)

	task := Task{Name: "to be deleted"}
	if err := s.insertTask(&task); err != nil {
		t.Fatalf("insertTask: %v", err)
	}
	originalID := task.ID

	if err := s.deleteTask(task); err != nil {
		t.Fatalf("deleteTask: %v", err)
	}

	tasks, err := s.findTasks(TaskQuery{})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks after delete, got %d", len(tasks))
	}

	if err := s.restoreTask(task); err != nil {
		t.Fatalf("restoreTask: %v", err)
	}

	tasks, err = s.findTasks(TaskQuery{})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task after restore, got %d", len(tasks))
	}
	if tasks[0].ID != originalID {
		t.Fatalf("expected restored task ID %d, got %d", originalID, tasks[0].ID)
	}
	if tasks[0].Name != "to be deleted" {
		t.Fatalf("expected restored task name %q, got %q", "to be deleted", tasks[0].Name)
	}
}

func TestCompletedAtHideRule(t *testing.T) {
	s := newTestStore(t)

	task := Task{Name: "old task"}
	if err := s.insertTask(&task); err != nil {
		t.Fatalf("insertTask: %v", err)
	}

	tenDaysAgo := time.Now().AddDate(0, 0, -10).Format("2006-01-02 15:04:05")
	if _, err := s.conn.Exec(`UPDATE tasks SET created_at = ? WHERE id = ?`, tenDaysAgo, task.ID); err != nil {
		t.Fatalf("backdate created_at: %v", err)
	}

	// Mark complete just now - should still appear since it was completed recently.
	if err := s.updateTaskCompletion(task.ID, 1); err != nil {
		t.Fatalf("updateTaskCompletion: %v", err)
	}

	tasks, err := s.findTasks(TaskQuery{})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected task completed moments ago to still be visible, got %d tasks", len(tasks))
	}

	// Force completed_at to 2 days ago - should now be hidden.
	twoDaysAgo := time.Now().AddDate(0, 0, -2).Format("2006-01-02 15:04:05")
	if _, err := s.conn.Exec(`UPDATE tasks SET completed_at = ? WHERE id = ?`, twoDaysAgo, task.ID); err != nil {
		t.Fatalf("backdate completed_at: %v", err)
	}

	tasks, err = s.findTasks(TaskQuery{})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected task completed 2 days ago to be hidden, got %d tasks", len(tasks))
	}
}

func TestCompletedAtHideRule_UsesLocalTime(t *testing.T) {
	s := newTestStore(t)

	// 23 hours ago in local time: should still be visible even in a
	// timezone far from UTC. Under a bare datetime('now') comparison this
	// would also pass in a UTC+ zone, so this case alone doesn't prove much
	// - see the 25-hour case below for the one that actually catches the bug.
	task23 := Task{Name: "23 hours ago"}
	if err := s.insertTask(&task23); err != nil {
		t.Fatalf("insertTask: %v", err)
	}
	if err := s.updateTaskCompletion(task23.ID, 1); err != nil {
		t.Fatalf("updateTaskCompletion: %v", err)
	}
	twentyThreeHoursAgo := time.Now().Add(-23 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err := s.conn.Exec(`UPDATE tasks SET completed_at = ? WHERE id = ?`, twentyThreeHoursAgo, task23.ID); err != nil {
		t.Fatalf("backdate completed_at: %v", err)
	}

	tasks, err := s.findTasks(TaskQuery{})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	found := false
	for _, tsk := range tasks {
		if tsk.ID == task23.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected task completed 23 hours ago (local time) to still be visible")
	}

	// 25 hours ago in local time: should be hidden. In a UTC+5:30 zone, a
	// bare datetime('now') comparison (UTC) would still show this task
	// because "25 hours ago local" is only ~19.5 hours ago in UTC - this is
	// the case that fails when the clause says UTC instead of localtime.
	task25 := Task{Name: "25 hours ago"}
	if err := s.insertTask(&task25); err != nil {
		t.Fatalf("insertTask: %v", err)
	}
	if err := s.updateTaskCompletion(task25.ID, 1); err != nil {
		t.Fatalf("updateTaskCompletion: %v", err)
	}
	twentyFiveHoursAgo := time.Now().Add(-25 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err := s.conn.Exec(`UPDATE tasks SET completed_at = ? WHERE id = ?`, twentyFiveHoursAgo, task25.ID); err != nil {
		t.Fatalf("backdate completed_at: %v", err)
	}

	tasks, err = s.findTasks(TaskQuery{})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	for _, tsk := range tasks {
		if tsk.ID == task25.ID {
			t.Fatalf("expected task completed 25 hours ago (local time) to be hidden")
		}
	}
}

func TestGetOrCreateProject_UpsertsByRootPath(t *testing.T) {
	s := newTestStore(t)

	root := &ProjectRoot{Path: "/tmp/example-project", Name: "example-project", GitRemote: "", Marker: ".git"}

	first, err := s.getOrCreateProject(root)
	if err != nil {
		t.Fatalf("getOrCreateProject (first): %v", err)
	}
	if first.ID == 0 {
		t.Fatalf("expected non-zero project ID")
	}

	second, err := s.getOrCreateProject(root)
	if err != nil {
		t.Fatalf("getOrCreateProject (second): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected same project ID on second call, got %d and %d", first.ID, second.ID)
	}

	projects, err := s.getAllProjects()
	if err != nil {
		t.Fatalf("getAllProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected exactly 1 project row after two getOrCreateProject calls, got %d", len(projects))
	}
}

func TestFindTasks_ProjectScoping(t *testing.T) {
	s := newTestStore(t)

	root := &ProjectRoot{Path: "/tmp/scoped-project", Name: "scoped-project", GitRemote: "", Marker: ".git"}
	proj, err := s.getOrCreateProject(root)
	if err != nil {
		t.Fatalf("getOrCreateProject: %v", err)
	}

	projectTask := Task{Name: "in project", ProjectID: sql.NullInt64{Int64: proj.ID, Valid: true}}
	if err := s.insertTask(&projectTask); err != nil {
		t.Fatalf("insertTask (project task): %v", err)
	}

	inboxTask := Task{Name: "in inbox"}
	if err := s.insertTask(&inboxTask); err != nil {
		t.Fatalf("insertTask (inbox task): %v", err)
	}

	id := proj.ID
	scoped, err := s.findTasks(TaskQuery{ProjectID: &id})
	if err != nil {
		t.Fatalf("findTasks(ProjectID): %v", err)
	}
	if len(scoped) != 1 || scoped[0].ID != projectTask.ID {
		t.Fatalf("expected only the project task from findTasks(ProjectID: &id), got %+v", scoped)
	}

	inbox, err := s.findTasks(TaskQuery{NoProject: true})
	if err != nil {
		t.Fatalf("findTasks(NoProject): %v", err)
	}
	if len(inbox) != 1 || inbox[0].ID != inboxTask.ID {
		t.Fatalf("expected only the inbox task from findTasks(NoProject: true), got %+v", inbox)
	}
}

func TestCompleteTask_RecurringSpawnsSuccessor(t *testing.T) {
	s := newTestStore(t)

	due := time.Date(2026, 8, 10, 9, 0, 0, 0, time.Local) // Monday
	task := Task{Name: "standup", RecurRule: "every monday", DueAt: sql.NullTime{Time: due, Valid: true}}
	if err := s.insertTask(&task); err != nil {
		t.Fatalf("insertTask: %v", err)
	}

	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.Local) // same day, later
	newID, err := s.completeTask(task, now)
	if err != nil {
		t.Fatalf("completeTask: %v", err)
	}
	if newID == 0 {
		t.Fatalf("expected a spawned successor, got 0")
	}

	tasks, err := s.findTasks(TaskQuery{IncludeDone: true})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks (completed original + successor), got %d", len(tasks))
	}

	successor := findTaskInSlice(tasks, newID)
	if successor == nil {
		t.Fatalf("successor task %d not found", newID)
	}
	if successor.Completed != 0 {
		t.Errorf("expected successor to be incomplete")
	}
	if successor.Status != "todo" {
		t.Errorf("expected successor status todo, got %q", successor.Status)
	}
	wantDue := time.Date(2026, 8, 17, 9, 0, 0, 0, time.Local)
	if !successor.DueAt.Valid || !successor.DueAt.Time.Equal(wantDue) {
		t.Errorf("successor DueAt = %v, want %v", successor.DueAt, wantDue)
	}
	if successor.RecurRule != "every monday" {
		t.Errorf("successor RecurRule = %q, want %q", successor.RecurRule, "every monday")
	}

	original := findTaskInSlice(tasks, task.ID)
	if original == nil || original.Completed != 1 {
		t.Errorf("expected the original task to remain completed")
	}
}

func TestCompleteTask_NonRecurringSpawnsNone(t *testing.T) {
	s := newTestStore(t)

	task := Task{Name: "one-off"}
	if err := s.insertTask(&task); err != nil {
		t.Fatalf("insertTask: %v", err)
	}

	newID, err := s.completeTask(task, time.Now())
	if err != nil {
		t.Fatalf("completeTask: %v", err)
	}
	if newID != 0 {
		t.Fatalf("expected no spawn for a non-recurring task, got id %d", newID)
	}

	tasks, err := s.findTasks(TaskQuery{IncludeDone: true})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected exactly 1 task, got %d", len(tasks))
	}
	if tasks[0].Completed != 1 {
		t.Errorf("expected task to be marked complete")
	}
}

func TestCompleteTask_AlreadyCompletedIsNoOp(t *testing.T) {
	s := newTestStore(t)

	task := Task{Name: "standup", RecurRule: "every day"}
	if err := s.insertTask(&task); err != nil {
		t.Fatalf("insertTask: %v", err)
	}

	if _, err := s.completeTask(task, time.Now()); err != nil {
		t.Fatalf("completeTask (first): %v", err)
	}

	tasksAfterFirst, err := s.findTasks(TaskQuery{IncludeDone: true})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(tasksAfterFirst) != 2 {
		t.Fatalf("expected 2 tasks after first completeTask, got %d", len(tasksAfterFirst))
	}

	// A caller passing the now-completed task (as it would read back from
	// the store) must get a no-op, not a second successor.
	completed := task
	completed.Completed = 1
	newID, err := s.completeTask(completed, time.Now())
	if err != nil {
		t.Fatalf("completeTask (already completed): %v", err)
	}
	if newID != 0 {
		t.Fatalf("expected no-op (id 0) completing an already-completed task, got %d", newID)
	}

	tasksAfterSecond, err := s.findTasks(TaskQuery{IncludeDone: true})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(tasksAfterSecond) != len(tasksAfterFirst) {
		t.Fatalf("expected no new tasks from the double-spawn guard: had %d, now have %d", len(tasksAfterFirst), len(tasksAfterSecond))
	}
}

func TestUncompleteNeverSpawns(t *testing.T) {
	s := newTestStore(t)

	task := Task{Name: "standup", RecurRule: "every day"}
	if err := s.insertTask(&task); err != nil {
		t.Fatalf("insertTask: %v", err)
	}

	if _, err := s.completeTask(task, time.Now()); err != nil {
		t.Fatalf("completeTask: %v", err)
	}

	// Un-complete via updateTaskCompletion, exactly like the TUI's 1 -> 0
	// path - must never spawn another successor.
	if err := s.updateTaskCompletion(task.ID, 0); err != nil {
		t.Fatalf("updateTaskCompletion: %v", err)
	}

	tasks, err := s.findTasks(TaskQuery{IncludeDone: true})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected still exactly 2 tasks after un-completing, got %d", len(tasks))
	}
}

func TestCompleteTask_OldSeriesRollsToFutureDate(t *testing.T) {
	s := newTestStore(t)

	// Weekly standup, due 5 weeks ago, left uncompleted until now.
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local)
	due := now.AddDate(0, 0, -35)
	task := Task{Name: "standup", RecurRule: "every week", DueAt: sql.NullTime{Time: due, Valid: true}}
	if err := s.insertTask(&task); err != nil {
		t.Fatalf("insertTask: %v", err)
	}

	newID, err := s.completeTask(task, now)
	if err != nil {
		t.Fatalf("completeTask: %v", err)
	}
	if newID == 0 {
		t.Fatalf("expected a spawned successor")
	}

	tasks, err := s.findTasks(TaskQuery{IncludeDone: true})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	successor := findTaskInSlice(tasks, newID)
	if successor == nil {
		t.Fatalf("successor not found")
	}
	if !successor.DueAt.Valid {
		t.Fatalf("expected successor to have a due date")
	}
	if !successor.DueAt.Time.After(now) {
		t.Errorf("expected successor due date %v to be strictly after now (%v), not stuck in the past", successor.DueAt.Time, now)
	}
}

func TestCompleteTask_SuccessorInheritsCategoryProjectPriority(t *testing.T) {
	s := newTestStore(t)

	cat, err := s.createCategory("work")
	if err != nil {
		t.Fatalf("createCategory: %v", err)
	}
	root := &ProjectRoot{Path: "/tmp/recur-project", Name: "recur-project", Marker: ".git"}
	proj, err := s.getOrCreateProject(root)
	if err != nil {
		t.Fatalf("getOrCreateProject: %v", err)
	}

	task := Task{
		Name:       "standup",
		RecurRule:  "every day",
		CategoryID: sql.NullInt64{Int64: cat.ID, Valid: true},
		ProjectID:  sql.NullInt64{Int64: proj.ID, Valid: true},
		Priority:   PriorityHigh,
	}
	if err := s.insertTask(&task); err != nil {
		t.Fatalf("insertTask: %v", err)
	}

	newID, err := s.completeTask(task, time.Now())
	if err != nil {
		t.Fatalf("completeTask: %v", err)
	}
	if newID == 0 {
		t.Fatalf("expected a spawned successor")
	}

	tasks, err := s.findTasks(TaskQuery{IncludeDone: true})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	successor := findTaskInSlice(tasks, newID)
	if successor == nil {
		t.Fatalf("successor not found")
	}
	if !successor.CategoryID.Valid || successor.CategoryID.Int64 != cat.ID {
		t.Errorf("successor CategoryID = %v, want %d", successor.CategoryID, cat.ID)
	}
	if !successor.ProjectID.Valid || successor.ProjectID.Int64 != proj.ID {
		t.Errorf("successor ProjectID = %v, want %d", successor.ProjectID, proj.ID)
	}
	if successor.Priority != PriorityHigh {
		t.Errorf("successor Priority = %d, want %d", successor.Priority, PriorityHigh)
	}
}

func findTaskInSlice(tasks []Task, id int64) *Task {
	for i := range tasks {
		if tasks[i].ID == id {
			return &tasks[i]
		}
	}
	return nil
}

func TestSavedViews_SaveGetDelete(t *testing.T) {
	s := newTestStore(t)

	views, err := s.getSavedViews()
	if err != nil {
		t.Fatalf("getSavedViews (built-ins only): %v", err)
	}
	if len(views) != len(builtInViews) {
		t.Fatalf("expected %d built-in views before any user view is saved, got %d", len(builtInViews), len(views))
	}

	spec := ViewSpec{Due: "today", Category: "work"}
	if err := s.saveView("My View", spec); err != nil {
		t.Fatalf("saveView: %v", err)
	}

	views, err = s.getSavedViews()
	if err != nil {
		t.Fatalf("getSavedViews: %v", err)
	}
	if len(views) != len(builtInViews)+1 {
		t.Fatalf("expected %d views after saving one, got %d", len(builtInViews)+1, len(views))
	}

	saved := views[len(views)-1]
	if saved.Name != "My View" {
		t.Errorf("saved.Name = %q, want %q", saved.Name, "My View")
	}
	if saved.BuiltIn {
		t.Errorf("expected the saved view to have BuiltIn = false")
	}
	if !reflect.DeepEqual(saved.Spec, spec) {
		t.Errorf("saved.Spec = %+v, want %+v", saved.Spec, spec)
	}
	if saved.Position != len(builtInViews)+1 {
		t.Errorf("saved.Position = %d, want %d", saved.Position, len(builtInViews)+1)
	}

	if err := s.deleteView(saved.ID); err != nil {
		t.Fatalf("deleteView: %v", err)
	}

	views, err = s.getSavedViews()
	if err != nil {
		t.Fatalf("getSavedViews after delete: %v", err)
	}
	if len(views) != len(builtInViews) {
		t.Fatalf("expected %d views after delete, got %d", len(builtInViews), len(views))
	}
}

// TestSavedViews_PositionsStayContiguous proves getSavedViews always
// renumbers Position sequentially over built-ins + user views, even after a
// user view in the middle is deleted and another is added.
func TestSavedViews_PositionsStayContiguous(t *testing.T) {
	s := newTestStore(t)

	if err := s.saveView("Alpha", ViewSpec{Due: "today"}); err != nil {
		t.Fatalf("saveView(Alpha): %v", err)
	}
	if err := s.saveView("Beta", ViewSpec{Due: "tomorrow"}); err != nil {
		t.Fatalf("saveView(Beta): %v", err)
	}
	if err := s.saveView("Gamma", ViewSpec{Due: "week"}); err != nil {
		t.Fatalf("saveView(Gamma): %v", err)
	}

	views, err := s.getSavedViews()
	if err != nil {
		t.Fatalf("getSavedViews: %v", err)
	}
	beta := views[len(builtInViews)+1]
	if beta.Name != "Beta" {
		t.Fatalf("expected Beta at index %d, got %+v", len(builtInViews)+1, beta)
	}
	if err := s.deleteView(beta.ID); err != nil {
		t.Fatalf("deleteView(Beta): %v", err)
	}

	if err := s.saveView("Delta", ViewSpec{Due: "overdue"}); err != nil {
		t.Fatalf("saveView(Delta): %v", err)
	}

	views, err = s.getSavedViews()
	if err != nil {
		t.Fatalf("getSavedViews after delete+add: %v", err)
	}
	wantNames := []string{"Today", "Overdue", "Next 7 days", "Blocked", "Alpha", "Gamma", "Delta"}
	if len(views) != len(wantNames) {
		t.Fatalf("expected %d views, got %d: %+v", len(wantNames), len(views), views)
	}
	for i, v := range views {
		if v.Name != wantNames[i] {
			t.Errorf("views[%d].Name = %q, want %q", i, v.Name, wantNames[i])
		}
		if v.Position != i+1 {
			t.Errorf("views[%d].Position = %d, want %d (positions must stay contiguous)", i, v.Position, i+1)
		}
	}
}

// TestSavedViews_DuplicateNameRejected proves the case-insensitive UNIQUE
// constraint on views.name is enforced, not silently overwritten.
func TestSavedViews_DuplicateNameRejected(t *testing.T) {
	s := newTestStore(t)

	if err := s.saveView("Focus", ViewSpec{Due: "today"}); err != nil {
		t.Fatalf("saveView (first): %v", err)
	}
	if err := s.saveView("focus", ViewSpec{Due: "overdue"}); err == nil {
		t.Fatalf("expected saveView to reject a case-insensitive duplicate name")
	}

	views, err := s.getSavedViews()
	if err != nil {
		t.Fatalf("getSavedViews: %v", err)
	}
	if len(views) != len(builtInViews)+1 {
		t.Fatalf("expected the rejected duplicate to not be saved, got %d views", len(views))
	}
}

// TestSavedViews_BuiltInNameRejected proves saveView itself rejects a name
// that collides (case-insensitively) with a built-in view - not just the
// CLI/TUI callers that also check first - since a user row shadowing "Today"
// would silently break what the "1" key and the palette's "View: Today"
// command are supposed to activate.
func TestSavedViews_BuiltInNameRejected(t *testing.T) {
	s := newTestStore(t)

	for _, name := range []string{"Today", "today", "BLOCKED"} {
		if err := s.saveView(name, ViewSpec{Due: "week"}); err == nil {
			t.Errorf("expected saveView(%q) to be rejected as a built-in name", name)
		} else if !errors.Is(err, errBuiltInViewName) {
			t.Errorf("saveView(%q) error = %v, want it to wrap errBuiltInViewName", name, err)
		}
	}

	views, err := s.getSavedViews()
	if err != nil {
		t.Fatalf("getSavedViews: %v", err)
	}
	if len(views) != len(builtInViews) {
		t.Fatalf("expected no user views saved, got %d views", len(views))
	}
}

func TestCategoryDeleteNullsForeignKey(t *testing.T) {
	s := newTestStore(t)

	cat, err := s.createCategory("work")
	if err != nil {
		t.Fatalf("createCategory: %v", err)
	}

	task := Task{Name: "categorized task", CategoryID: sql.NullInt64{Int64: cat.ID, Valid: true}}
	if err := s.insertTask(&task); err != nil {
		t.Fatalf("insertTask: %v", err)
	}

	if err := s.deleteCategory(cat.ID); err != nil {
		t.Fatalf("deleteCategory: %v", err)
	}

	tasks, err := s.findTasks(TaskQuery{})
	if err != nil {
		t.Fatalf("findTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected task to survive category delete, got %d tasks", len(tasks))
	}
	if tasks[0].CategoryID.Valid {
		t.Fatalf("expected category_id to be NULL after category delete, got %v", tasks[0].CategoryID)
	}
}
