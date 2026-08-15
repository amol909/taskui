package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Command is one entry in the command palette (viewPalette, opened with
// ctrl+k). Run receives a pointer to the live model so it can mutate it in
// place exactly like the key handlers in main.go do, then must return a
// dereferenced *m as the tea.Model - the same (tea.Model, tea.Cmd) shape
// bubbletea's Update expects.
type Command struct {
	ID       string
	Title    string   // "Set status: blocked"
	Group    string   // "Task" | "View" | "Project" | "App"
	Keywords []string // extra search terms
	Run      func(m *model) (tea.Model, tea.Cmd)
}

// commandGroupOrder is the fixed display order the palette renders groups
// in - Task first, since task commands are what's most likely to be needed
// with a specific row already selected, down to App/Quit last.
var commandGroupOrder = map[string]int{"Task": 0, "View": 1, "Project": 2, "App": 3}

// commandKeyHints maps a Command.ID to the existing keybinding it duplicates
// (Command has no Key field of its own - see palette_test.go/Task 2's exact
// struct), so the palette can render it on the right of each row and teach
// the shortcut rather than replace it. Commands with no direct single-key
// equivalent (e.g. "set status: blocked" - only the cycle has a key) are
// simply absent here.
var commandKeyHints = map[string]string{
	"add-task":            "a",
	"toggle-all-projects": "A",
	"switch-project":      "P",
	"open-agenda":         "g",
	"filter-category":     "f",
	"manage-categories":   "c",
	"undo-delete":         "u",
	"cycle-status":        "s",
	"delete-task":         "d",
	"toggle-complete":     "enter",
	"quit":                "q",
}

// keyHintFor returns the key binding to render next to a command, or "" for
// none.
func keyHintFor(id string) string {
	if strings.HasPrefix(id, "view-") {
		// "view-3" -> "3": saved views at positions 1-9 are one keystroke
		// from the list view, same discipline as the fixed commands above.
		if n := strings.TrimPrefix(id, "view-"); len(n) == 1 && n[0] >= '1' && n[0] <= '9' {
			return n
		}
	}
	return commandKeyHints[id]
}

// commandsFor builds the command list for the current model state. Task-
// specific commands (status, priority, delete, toggle complete, ...) only
// appear when a task is under the cursor; "undo delete" only appears when
// there is something to undo.
func commandsFor(m *model) []Command {
	var cmds []Command

	cmds = append(cmds,
		Command{
			ID: "add-task", Title: "Add task", Group: "Task",
			Keywords: []string{"new", "create", "capture"},
			Run: func(m *model) (tea.Model, tea.Cmd) {
				m.view = viewCapture
				m.captureEditing = nil
				m.captureInput.SetValue("")
				m.captureInput.Focus()
				m.createMore = false
				return *m, textinput.Blink
			},
		},
		Command{
			ID: "toggle-all-projects", Title: "Toggle all-projects scope", Group: "Project",
			Keywords: []string{"scope", "everywhere"},
			Run: func(m *model) (tea.Model, tea.Cmd) {
				if m.scope == scopeAllProjects {
					m.scope = m.scopeBeforeAll
				} else {
					m.scopeBeforeAll = m.scope
					m.scope = scopeAllProjects
				}
				m.refreshTasks()
				m.cursor = -1
				if len(m.tasks) > 0 {
					m.cursor = 0
				}
				m.view = viewList
				return *m, nil
			},
		},
		Command{
			ID: "switch-project", Title: "Switch project", Group: "Project",
			Run: func(m *model) (tea.Model, tea.Cmd) {
				projects, err := m.store.getAllProjects()
				if err == nil {
					m.projects = projects
				}
				m.view = viewProjectSwitcher
				m.projectSwitcherCursor = 0
				return *m, nil
			},
		},
		Command{
			ID: "open-agenda", Title: "Open agenda", Group: "View",
			Run: func(m *model) (tea.Model, tea.Cmd) {
				m.view = viewAgenda
				flat := flattenAgendaTasks(bucketAgenda(m.tasks, time.Now()))
				m.cursor = -1
				if len(flat) > 0 {
					m.cursor = 0
				}
				return *m, nil
			},
		},
		Command{
			ID: "filter-category", Title: "Filter by category", Group: "View",
			Run: func(m *model) (tea.Model, tea.Cmd) {
				m.view = viewFilter
				m.filterInput.Focus()
				m.filterInput.SetValue("")
				m.categoryCursor = -1
				m.updateFilteredCategories()
				return *m, textinput.Blink
			},
		},
		Command{
			ID: "manage-categories", Title: "Manage categories", Group: "View",
			Run: func(m *model) (tea.Model, tea.Cmd) {
				m.view = viewCategoryManager
				m.refreshCategories()
				m.categoryManagerCursor = 0
				if len(m.categories) == 0 {
					m.categoryManagerCursor = -1
				}
				return *m, nil
			},
		},
		Command{
			ID: "save-view", Title: "Save current view…", Group: "View",
			Keywords: []string{"save", "bookmark", "new view"},
			Run: func(m *model) (tea.Model, tea.Cmd) {
				m.view = viewSaveView
				m.saveViewInput.SetValue("")
				m.saveViewInput.Focus()
				m.saveViewError = ""
				return *m, textinput.Blink
			},
		},
		Command{
			ID: "quit", Title: "Quit", Group: "App",
			Run: func(m *model) (tea.Model, tea.Cmd) { return *m, tea.Quit },
		},
	)

	if len(m.undoStack) > 0 {
		cmds = append(cmds, Command{
			ID: "undo-delete", Title: "Undo delete", Group: "Task",
			Run: func(m *model) (tea.Model, tea.Cmd) {
				if len(m.undoStack) == 0 {
					return *m, nil
				}
				taskToRestore := m.undoStack[len(m.undoStack)-1]
				m.undoStack = m.undoStack[:len(m.undoStack)-1]
				if err := m.store.restoreTask(taskToRestore); err == nil {
					m.refreshTasks()
					for i, t := range m.tasks {
						if t.ID == taskToRestore.ID {
							m.cursor = i
							break
						}
					}
				}
				m.view = viewList
				return *m, nil
			},
		})
	}

	if m.cursor >= 0 && m.cursor < len(m.tasks) {
		task := m.tasks[m.cursor]

		cmds = append(cmds, Command{
			ID: "cycle-status", Title: "Cycle status", Group: "Task",
			Run: func(m *model) (tea.Model, tea.Cmd) {
				if err := m.store.updateTaskStatus(task.ID, nextStatus(task.Status)); err == nil {
					m.refreshTasks()
				}
				m.view = viewList
				return *m, nil
			},
		})

		for _, status := range []string{"todo", "in-progress", "blocked"} {
			if status == task.Status {
				continue
			}
			status := status
			cmds = append(cmds, Command{
				ID: "set-status-" + status, Title: "Set status: " + status, Group: "Task",
				Run: func(m *model) (tea.Model, tea.Cmd) {
					if err := m.store.updateTaskStatus(task.ID, status); err == nil {
						m.refreshTasks()
					}
					m.view = viewList
					return *m, nil
				},
			})
		}

		priorities := []struct {
			val   int
			label string
		}{
			{PriorityNone, "none"}, {PriorityLow, "low"}, {PriorityMed, "medium"}, {PriorityHigh, "high"},
		}
		for _, p := range priorities {
			if p.val == task.Priority {
				continue
			}
			p := p
			cmds = append(cmds, Command{
				ID: fmt.Sprintf("set-priority-%d", p.val), Title: "Set priority: " + p.label, Group: "Task",
				Run: func(m *model) (tea.Model, tea.Cmd) {
					updated := task
					updated.Priority = p.val
					if err := m.store.updateTask(updated); err == nil {
						m.refreshTasks()
					}
					m.view = viewList
					return *m, nil
				},
			})
		}

		cmds = append(cmds, Command{
			ID: "delete-task", Title: "Delete task", Group: "Task",
			Run: func(m *model) (tea.Model, tea.Cmd) {
				if err := m.store.deleteTask(task); err == nil {
					m.undoStack = append(m.undoStack, task)
					m.refreshTasks()
					if m.cursor >= len(m.tasks) && len(m.tasks) > 0 {
						m.cursor = len(m.tasks) - 1
					} else if len(m.tasks) == 0 {
						m.cursor = -1
					}
				}
				m.view = viewList
				return *m, nil
			},
		})

		completeTitle := "Mark complete"
		if task.Completed == 1 {
			completeTitle = "Mark incomplete"
		}
		cmds = append(cmds, Command{
			ID: "toggle-complete", Title: completeTitle, Group: "Task",
			Keywords: []string{"done", "complete", "toggle"},
			Run: func(m *model) (tea.Model, tea.Cmd) {
				if task.Completed == 0 {
					if _, err := m.store.completeTask(task, time.Now()); err == nil {
						m.refreshTasks()
					}
				} else {
					if err := m.store.updateTaskCompletion(task.ID, 0); err == nil {
						m.refreshTasks()
					}
				}
				m.view = viewList
				return *m, nil
			},
		})
	}

	for _, v := range m.savedViews {
		v := v
		cmds = append(cmds, Command{
			ID: fmt.Sprintf("view-%d", v.Position), Title: "View: " + v.Name, Group: "View",
			Run: func(m *model) (tea.Model, tea.Cmd) {
				m.activateSavedView(v)
				return *m, nil
			},
		})
	}

	// One "Delete view: <name>" per user-defined view, never for built-ins -
	// the palette is already a picker, so this is the deletion UI rather
	// than a separate one.
	for _, v := range m.savedViews {
		if v.BuiltIn {
			continue
		}
		v := v
		cmds = append(cmds, Command{
			ID: fmt.Sprintf("delete-view-%d", v.ID), Title: "Delete view: " + v.Name, Group: "View",
			Run: func(m *model) (tea.Model, tea.Cmd) {
				if err := m.store.deleteView(v.ID); err == nil {
					if m.activeView != nil && m.activeView.ID == v.ID {
						m.activeView = nil
					}
					if views, verr := m.store.getSavedViews(); verr == nil {
						m.savedViews = views
					}
					m.resetCursorAfterRefresh()
				}
				m.view = viewList
				return *m, nil
			},
		})
	}

	return cmds
}

// isWordBoundary reports whether r starts a new word for fuzzyMatch's
// boundary bonus: the start of the haystack, or right after a space, '-' or
// ':'.
func isWordBoundaryChar(r rune) bool {
	return r == ' ' || r == '-' || r == ':'
}

// fuzzyMatch scores needle against haystack. ok=false means haystack doesn't
// contain needle's characters as a case-insensitive subsequence. Higher
// score = better. An empty needle matches everything with score 0.
//
// Matching is greedy left-to-right (each needle character binds to the
// first available haystack character after the previous match), then scored
// along that alignment: a flat bonus per matched character, a larger bonus
// for each pair of matches that land on consecutive haystack characters (a
// "run"), a bonus for a match landing right at a word boundary, and a small
// penalty per skipped character between two matches. That combination is
// what makes an exact, boundary-aligned prefix ("set" at the start of "Set
// status: blocked") heavily outscore the same letters scattered across
// several words.
func fuzzyMatch(needle, haystack string) (score int, ok bool) {
	if needle == "" {
		return 0, true
	}

	const (
		charScore        = 5
		consecutiveBonus = 10
		boundaryBonus    = 8
		gapPenalty       = 1
	)

	n := []rune(strings.ToLower(needle))
	h := []rune(strings.ToLower(haystack))

	searchFrom := 0
	prevMatch := -1
	for _, nc := range n {
		found := -1
		for j := searchFrom; j < len(h); j++ {
			if h[j] == nc {
				found = j
				break
			}
		}
		if found == -1 {
			return 0, false
		}

		score += charScore
		if prevMatch != -1 {
			gap := found - prevMatch
			if gap == 1 {
				score += consecutiveBonus
			} else {
				score -= (gap - 1) * gapPenalty
			}
		}
		if found == 0 || isWordBoundaryChar(h[found-1]) {
			score += boundaryBonus
		}

		prevMatch = found
		searchFrom = found + 1
	}

	return score, true
}

// matchCommand scores a command against needle: the best of its Title and
// each Keyword, since Keywords exist purely to catch synonyms Title doesn't
// spell out (e.g. "new"/"create" for "Add task").
func matchCommand(c Command, needle string) (score int, ok bool) {
	score, ok = fuzzyMatch(needle, c.Title)
	for _, kw := range c.Keywords {
		if s, kok := fuzzyMatch(needle, kw); kok && (!ok || s > score) {
			score, ok = s, true
		}
	}
	return score, ok
}

// rankCommands filters cmds to those matching needle and orders them the
// way the palette renders them: grouped by Group in commandGroupOrder,
// best match first within each group, ties broken by Title for a stable
// order. An empty needle keeps every command, in the same grouped order.
func rankCommands(cmds []Command, needle string) []Command {
	type scored struct {
		cmd   Command
		score int
	}

	matches := make([]scored, 0, len(cmds))
	for _, c := range cmds {
		if score, ok := matchCommand(c, needle); ok {
			matches = append(matches, scored{c, score})
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		gi, gj := commandGroupOrder[matches[i].cmd.Group], commandGroupOrder[matches[j].cmd.Group]
		if gi != gj {
			return gi < gj
		}
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].cmd.Title < matches[j].cmd.Title
	})

	out := make([]Command, len(matches))
	for i, s := range matches {
		out[i] = s.cmd
	}
	return out
}
