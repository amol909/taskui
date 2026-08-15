package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	viewList = iota
	viewCapture
	viewFilter
	viewCategoryManager
	viewCategoryManagerEdit
	viewProjectSwitcher
	viewAgenda
	viewPalette
	viewSaveView
)

type scopeMode int

const (
	scopeCurrentProject scopeMode = iota // m.scopeProject
	scopeAllProjects
	scopeInbox
)

type KeyMap struct {
	Up              key.Binding
	Down            key.Binding
	NavUp           key.Binding
	NavDown         key.Binding
	Add             key.Binding
	More            key.Binding
	Delete          key.Binding
	Edit            key.Binding
	Filter          key.Binding
	Enter           key.Binding
	Escape          key.Binding
	Undo            key.Binding
	Quit            key.Binding
	Help            key.Binding
	CategoryManager key.Binding
	Status          key.Binding
	ScopeAll        key.Binding
	Projects        key.Binding
	Agenda          key.Binding
	Palette         key.Binding
	ViewSlots       key.Binding
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Add, k.Edit, k.Delete, k.Filter, k.Status, k.Enter, k.Escape},
		{k.More, k.Undo, k.ScopeAll, k.Projects, k.Agenda, k.Palette, k.ViewSlots, k.Help, k.Quit},
	}
}

var DefaultKeyMap = KeyMap{
	Up: key.NewBinding(
		key.WithKeys("k", "up"),
		key.WithHelp("↑/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("j", "down"),
		key.WithHelp("↓/j", "move down"),
	),
	NavUp: key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "move up"),
	),
	NavDown: key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "move down"),
	),
	Add: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "add task"),
	),
	More: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "create more"),
	),
	Delete: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "delete task"),
	),
	Edit: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "edit task"),
	),
	Filter: key.NewBinding(
		key.WithKeys("f", "/"),
		key.WithHelp("f", "filter"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
	Undo: key.NewBinding(
		key.WithKeys("u"),
		key.WithHelp("u", "undo delete"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	CategoryManager: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "manage categories"),
	),
	Status: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "cycle status"),
	),
	ScopeAll: key.NewBinding(
		key.WithKeys("A"),
		key.WithHelp("A", "all projects"),
	),
	Projects: key.NewBinding(
		key.WithKeys("P"),
		key.WithHelp("P", "switch project"),
	),
	Agenda: key.NewBinding(
		key.WithKeys("g"),
		key.WithHelp("g", "agenda view"),
	),
	Palette: key.NewBinding(
		key.WithKeys("ctrl+k"),
		key.WithHelp("ctrl+k", "command palette"),
	),
	ViewSlots: key.NewBinding(
		key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9"),
		key.WithHelp("1-9", "saved views"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
}

type model struct {
	view    int
	tasks   []Task
	cursor  int
	store   *Store
	loadErr error

	// Capture: single-line add/edit input, parsed via ParseTask.
	captureInput   textinput.Model
	captureEditing *Task // nil = adding a new task; non-nil = editing this task in place

	// Category autocomplete (used by the filter view's dropdown)
	categories         []Category
	filteredCategories []Category
	categoryCursor     int
	createMore         bool

	// Filter state
	filterCategory *Category
	filterInput    textinput.Model
	allTasks       []Task

	// Category manager state
	categoryManagerCursor int
	editingCategory       *Category
	categoryEditInput     textinput.Model

	// Undo stack for deleted tasks (session-only)
	undoStack         []Task
	categoryUndoStack []Category

	// Scope state (project scoping)
	scope          scopeMode
	scopeProject   *Project  // the project currently being viewed
	launchProject  *Project  // resolved once at startup; nil when launched outside a project
	scopeBeforeAll scopeMode // scope to restore when toggling off scopeAllProjects

	// Project switcher state
	projects              []Project
	projectSwitcherCursor int

	// Saved views (built-ins + user-defined); activeView nil = plain scoped list
	savedViews []SavedView
	activeView *SavedView

	// Save-view name prompt (viewSaveView, opened via the palette's "Save
	// current view…"). saveViewError holds the reason the last enter press
	// was refused (empty name, empty spec, name collision, ...), shown
	// inline rather than silently doing nothing.
	saveViewInput textinput.Model
	saveViewError string

	// Command palette (viewPalette, opened with ctrl+k from the list and
	// agenda views). paletteReturnView is where esc, or a command that
	// doesn't itself change m.view, sends the user back to.
	paletteInput      textinput.Model
	paletteCursor     int
	paletteReturnView int

	// Terminal size
	width  int
	height int

	keyMap KeyMap
	help   help.Model
}

func initialModel(store *Store, launchProject *Project) model {
	scope := scopeInbox
	var scopeProject *Project
	if launchProject != nil {
		scope = scopeCurrentProject
		scopeProject = launchProject
	}

	categories, _ := store.getAllCategories()
	savedViews, _ := store.getSavedViews()

	captureInput := textinput.New()
	captureInput.Placeholder = "task  #category  tomorrow 5pm  !high"
	captureInput.CharLimit = 200
	captureInput.Width = 60

	filterInput := textinput.New()
	filterInput.Placeholder = "Filter by category..."
	filterInput.CharLimit = 50
	filterInput.Width = 40

	categoryEditInput := textinput.New()
	categoryEditInput.Placeholder = "Category name"
	categoryEditInput.CharLimit = 50
	categoryEditInput.Width = 40

	paletteInput := textinput.New()
	paletteInput.Placeholder = "Type a command…"
	paletteInput.CharLimit = 60
	paletteInput.Width = 50

	saveViewInput := textinput.New()
	saveViewInput.Placeholder = "View name"
	saveViewInput.CharLimit = 50
	saveViewInput.Width = 40

	m := model{
		view:                  viewList,
		store:                 store,
		captureInput:          captureInput,
		filterInput:           filterInput,
		categoryEditInput:     categoryEditInput,
		paletteInput:          paletteInput,
		saveViewInput:         saveViewInput,
		cursor:                -1,
		categories:            categories,
		savedViews:            savedViews,
		filteredCategories:    categories,
		categoryCursor:        -1,
		categoryManagerCursor: 0,
		scope:                 scope,
		scopeProject:          scopeProject,
		launchProject:         launchProject,
		scopeBeforeAll:        scope,
		keyMap:                DefaultKeyMap,
		help:                  help.New(),
		width:                 80,
		height:                24,
	}

	m.refreshTasks()

	return m
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *model) refreshTasks() {
	q, err := m.currentQuery()
	if err != nil {
		m.loadErr = err
		return
	}

	tasks, err := m.store.findTasks(q)
	if err != nil {
		m.loadErr = err
		return
	}
	m.tasks = tasks
	m.allTasks = tasks
	m.loadErr = nil
}

// currentQuery combines the scope, the category filter and any active saved
// view into a single TaskQuery.
func (m *model) currentQuery() (TaskQuery, error) {
	q := TaskQuery{}

	switch m.scope {
	case scopeCurrentProject:
		if m.scopeProject != nil {
			id := m.scopeProject.ID
			q.ProjectID = &id
		}
	case scopeInbox:
		q.NoProject = true
	case scopeAllProjects:
		// no project filter
	}

	if m.filterCategory != nil {
		q.CategoryID = &m.filterCategory.ID
	}

	if m.activeView == nil {
		return q, nil
	}

	vq, err := resolveViewSpec(m.activeView.Spec, time.Now(), m.launchProject, m.lookupCategory)
	if err != nil {
		return TaskQuery{}, err
	}

	// A view with an explicit scope replaces the current project filter; a
	// view with Scope "" inherits whatever scope the user is already in.
	if m.activeView.Spec.Scope != "" {
		q.ProjectID, q.NoProject = vq.ProjectID, vq.NoProject
	}
	if vq.CategoryID != nil {
		q.CategoryID = vq.CategoryID
	}
	q.Statuses = vq.Statuses
	q.DueAfter, q.DueBefore, q.DueSet = vq.DueAfter, vq.DueBefore, vq.DueSet
	q.DueBuckets = vq.DueBuckets
	q.IncludeDone = vq.IncludeDone

	return q, nil
}

func (m *model) lookupCategory(name string) *Category {
	cat, err := m.store.findCategoryByName(name)
	if err != nil {
		return nil
	}
	return cat
}

func (m *model) activateSavedView(v SavedView) {
	active := v
	m.activeView = &active
	m.resetCursorAfterRefresh()
}

func (m *model) clearSavedView() {
	m.activeView = nil
	m.resetCursorAfterRefresh()
}

// currentViewSpecForSave builds the ViewSpec that "Save current view…"
// persists: the scope/category the user is actually looking at, plus the
// Due/Statuses of whatever saved view (if any) is currently active - so
// "Today, narrowed to #work" saves as exactly that. Scope is always
// populated (m.scope is never a zero value), unlike Due/Statuses/Category,
// which stay unset unless there is something to carry over.
func (m *model) currentViewSpecForSave() ViewSpec {
	var spec ViewSpec

	switch m.scope {
	case scopeCurrentProject:
		spec.Scope = "project"
	case scopeAllProjects:
		spec.Scope = "all"
	case scopeInbox:
		spec.Scope = "inbox"
	}

	if m.filterCategory != nil {
		spec.Category = m.filterCategory.Name
	}

	if m.activeView != nil {
		spec.Due = m.activeView.Spec.Due
		spec.Statuses = append([]string(nil), m.activeView.Spec.Statuses...)
	}

	return spec
}

func (m *model) resetCursorAfterRefresh() {
	m.refreshTasks()
	m.cursor = -1
	if len(m.tasks) > 0 {
		m.cursor = 0
	}
}

func (m *model) refreshCategories() {
	categories, err := m.store.getAllCategories()
	if err == nil {
		m.categories = categories
	}
}

// updateFilteredCategories refreshes the category dropdown shown by the
// filter view (viewFilter is the only remaining caller).
func (m *model) updateFilteredCategories() {
	filtered, err := m.store.searchCategories(m.filterInput.Value())
	if err == nil {
		m.filteredCategories = filtered
	}

	if m.categoryCursor >= len(m.filteredCategories) {
		m.categoryCursor = len(m.filteredCategories) - 1
	}
	if m.categoryCursor < -1 {
		m.categoryCursor = -1
	}
}

func (m *model) resetInputState() {
	m.captureInput.SetValue("")
	m.captureInput.Blur()
	m.captureEditing = nil
	m.filterInput.SetValue("")
	m.filterInput.Blur()
	m.createMore = false
	m.categoryCursor = -1
	m.filteredCategories = m.categories
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	store := m.store

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch m.view {
		case viewList:
			return m.handleListView(msg)

		case viewCapture:
			return m.handleCaptureInput(msg, store)

		case viewFilter:
			return m.handleFilterView(msg)

		case viewCategoryManager:
			return m.handleCategoryManager(msg)

		case viewCategoryManagerEdit:
			return m.handleCategoryManagerEdit(msg)

		case viewProjectSwitcher:
			return m.handleProjectSwitcher(msg)

		case viewAgenda:
			return m.handleAgendaView(msg)

		case viewPalette:
			return m.handlePaletteView(msg)

		case viewSaveView:
			return m.handleSaveView(msg)
		}
	}

	return m, cmd
}

func (m model) handleListView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keyMap.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keyMap.ScopeAll):
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
		return m, nil

	case key.Matches(msg, m.keyMap.Projects):
		projects, err := m.store.getAllProjects()
		if err == nil {
			m.projects = projects
		}
		m.view = viewProjectSwitcher
		m.projectSwitcherCursor = 0
		return m, nil

	case key.Matches(msg, m.keyMap.Add):
		m.view = viewCapture
		m.captureEditing = nil
		m.captureInput.SetValue("")
		m.captureInput.Focus()
		m.createMore = false
		return m, textinput.Blink

	case key.Matches(msg, m.keyMap.Edit):
		if len(m.tasks) == 0 || m.cursor < 0 || m.cursor >= len(m.tasks) {
			return m, nil
		}
		task := m.tasks[m.cursor]
		m.view = viewCapture
		m.captureEditing = &task
		m.captureInput.SetValue(FormatTask(task))
		m.captureInput.CursorEnd()
		m.captureInput.Focus()
		m.createMore = false
		return m, textinput.Blink

	case key.Matches(msg, m.keyMap.Filter):
		m.view = viewFilter
		m.filterInput.Focus()
		m.filterInput.SetValue("")
		m.categoryCursor = -1
		m.updateFilteredCategories()
		return m, textinput.Blink

	case key.Matches(msg, m.keyMap.Agenda):
		m.view = viewAgenda
		flat := flattenAgendaTasks(bucketAgenda(m.tasks, time.Now()))
		m.cursor = -1
		if len(flat) > 0 {
			m.cursor = 0
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Palette):
		m.paletteReturnView = viewList
		m.view = viewPalette
		m.paletteInput.SetValue("")
		m.paletteInput.Focus()
		m.paletteCursor = 0
		return m, textinput.Blink

	case key.Matches(msg, m.keyMap.ViewSlots):
		m.activateViewSlot(msg)
		return m, nil

	case key.Matches(msg, m.keyMap.CategoryManager):
		m.view = viewCategoryManager
		m.refreshCategories()
		m.categoryManagerCursor = 0
		if len(m.categories) == 0 {
			m.categoryManagerCursor = -1
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Delete):
		if len(m.tasks) == 0 || m.cursor < 0 || m.cursor >= len(m.tasks) {
			return m, nil
		}
		taskToDelete := m.tasks[m.cursor]
		if err := m.store.deleteTask(taskToDelete); err == nil {
			m.undoStack = append(m.undoStack, taskToDelete)
			m.refreshTasks()
			if m.cursor >= len(m.tasks) && len(m.tasks) > 0 {
				m.cursor = len(m.tasks) - 1
			} else if len(m.tasks) == 0 {
				m.cursor = -1
			}
		}

	case key.Matches(msg, m.keyMap.Undo):
		if len(m.undoStack) == 0 {
			return m, nil
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

	case key.Matches(msg, m.keyMap.Up):
		if m.cursor > 0 {
			m.cursor--
		}

	case key.Matches(msg, m.keyMap.Down):
		if m.cursor < len(m.tasks)-1 {
			m.cursor++
		}

	case key.Matches(msg, m.keyMap.Enter):
		if len(m.tasks) == 0 || m.cursor < 0 || m.cursor >= len(m.tasks) {
			return m, nil
		}
		currentTask := m.tasks[m.cursor]
		if currentTask.Completed == 0 {
			// completeTask marks it complete and, when it recurs, spawns
			// the next occurrence - never on the un-complete path below.
			if _, err := m.store.completeTask(currentTask, time.Now()); err == nil {
				m.refreshTasks()
			}
		} else {
			if err := m.store.updateTaskCompletion(currentTask.ID, 0); err == nil {
				m.refreshTasks()
			}
		}

	case key.Matches(msg, m.keyMap.Status):
		if len(m.tasks) == 0 || m.cursor < 0 || m.cursor >= len(m.tasks) {
			return m, nil
		}
		currentTask := m.tasks[m.cursor]
		newStatus := nextStatus(currentTask.Status)
		if err := m.store.updateTaskStatus(currentTask.ID, newStatus); err == nil {
			m.refreshTasks()
		}

	case key.Matches(msg, m.keyMap.Escape):
		// esc peels off one layer at a time: an active saved view first,
		// then the category filter, then nothing.
		if m.activeView != nil {
			m.clearSavedView()
			return m, nil
		}
		if m.filterCategory != nil {
			m.filterCategory = nil
			m.refreshTasks()
			m.cursor = -1
			if len(m.tasks) > 0 {
				m.cursor = 0
			}
		}
	}

	return m, nil
}

// activateViewSlot activates the saved view at the position named by a
// "1"-"9" key press; a position with no view is a no-op.
func (m *model) activateViewSlot(msg tea.KeyMsg) {
	n, err := strconv.Atoi(msg.String())
	if err != nil {
		return
	}
	for _, v := range m.savedViews {
		if v.Position == n {
			m.activateSavedView(v)
			return
		}
	}
}

// handleAgendaView drives viewAgenda: the same task set as the list view
// (current scope + category filter), grouped into buckets by bucketAgenda.
// The cursor moves across the flattened bucket list, recomputed on every
// keystroke from m.tasks so it always matches what renderAgenda just drew;
// enter/d/s act on the task under the cursor exactly as they do in the list
// view.
func (m model) handleAgendaView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	flat := flattenAgendaTasks(bucketAgenda(m.tasks, time.Now()))

	switch {
	case key.Matches(msg, m.keyMap.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keyMap.Agenda), key.Matches(msg, m.keyMap.Escape):
		m.view = viewList
		m.cursor = -1
		if len(m.tasks) > 0 {
			m.cursor = 0
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Palette):
		m.paletteReturnView = viewAgenda
		m.view = viewPalette
		m.paletteInput.SetValue("")
		m.paletteInput.Focus()
		m.paletteCursor = 0
		return m, textinput.Blink

	case key.Matches(msg, m.keyMap.ViewSlots):
		m.activateViewSlot(msg)
		return m, nil

	case key.Matches(msg, m.keyMap.Up):
		if m.cursor > 0 {
			m.cursor--
		}

	case key.Matches(msg, m.keyMap.Down):
		if m.cursor < len(flat)-1 {
			m.cursor++
		}

	case key.Matches(msg, m.keyMap.Enter):
		if len(flat) == 0 || m.cursor < 0 || m.cursor >= len(flat) {
			return m, nil
		}
		currentTask := flat[m.cursor]
		if currentTask.Completed == 0 {
			if _, err := m.store.completeTask(currentTask, time.Now()); err == nil {
				m.refreshTasks()
			}
		} else {
			if err := m.store.updateTaskCompletion(currentTask.ID, 0); err == nil {
				m.refreshTasks()
			}
		}

	case key.Matches(msg, m.keyMap.Delete):
		if len(flat) == 0 || m.cursor < 0 || m.cursor >= len(flat) {
			return m, nil
		}
		taskToDelete := flat[m.cursor]
		if err := m.store.deleteTask(taskToDelete); err == nil {
			m.undoStack = append(m.undoStack, taskToDelete)
			m.refreshTasks()
			newFlat := flattenAgendaTasks(bucketAgenda(m.tasks, time.Now()))
			if m.cursor >= len(newFlat) && len(newFlat) > 0 {
				m.cursor = len(newFlat) - 1
			} else if len(newFlat) == 0 {
				m.cursor = -1
			}
		}

	case key.Matches(msg, m.keyMap.Status):
		if len(flat) == 0 || m.cursor < 0 || m.cursor >= len(flat) {
			return m, nil
		}
		currentTask := flat[m.cursor]
		newStatus := nextStatus(currentTask.Status)
		if err := m.store.updateTaskStatus(currentTask.ID, newStatus); err == nil {
			m.refreshTasks()
		}
	}

	return m, nil
}

// handlePaletteView drives viewPalette: a textinput at the top, ranked
// commands (rankCommands over commandsFor) below. Navigation is NavUp/
// NavDown (arrows + ctrl+p/ctrl+n) only - never Up/Down, which include j/k
// and would make it impossible to type a command containing either letter
// while the input is focused (the same bug Slice 0 fixed in the other input
// views). paletteCursor is intentionally left unbounded by the 8-row display
// cap, matching how viewFilter's categoryCursor already behaves relative to
// renderCategoryDropdown's 5-row cap - see renderPalette.
func (m model) handlePaletteView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	cmds := rankCommands(commandsFor(&m), m.paletteInput.Value())

	switch {
	case key.Matches(msg, m.keyMap.Escape):
		m.paletteInput.Blur()
		m.paletteInput.SetValue("")
		m.view = m.paletteReturnView
		return m, nil

	case key.Matches(msg, m.keyMap.NavUp):
		if m.paletteCursor > 0 {
			m.paletteCursor--
		}
		return m, nil

	case key.Matches(msg, m.keyMap.NavDown):
		if m.paletteCursor < len(cmds)-1 {
			m.paletteCursor++
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Enter):
		if m.paletteCursor < 0 || m.paletteCursor >= len(cmds) {
			return m, nil
		}
		prevView := m.view
		chosen := cmds[m.paletteCursor]
		newModel, runCmd := chosen.Run(&m)
		nm := newModel.(model)
		if nm.view == prevView {
			// The command didn't itself navigate anywhere - close the
			// palette back to whichever view opened it.
			nm.view = nm.paletteReturnView
		}
		nm.paletteInput.Blur()
		nm.paletteInput.SetValue("")
		return nm, runCmd
	}

	m.paletteInput, cmd = m.paletteInput.Update(msg)

	// Recompute against the new input value and clamp the cursor into range
	// now that the result list may have shrunk (or grown).
	newCmds := rankCommands(commandsFor(&m), m.paletteInput.Value())
	if m.paletteCursor >= len(newCmds) {
		m.paletteCursor = len(newCmds) - 1
	}
	if m.paletteCursor < 0 && len(newCmds) > 0 {
		m.paletteCursor = 0
	}

	return m, cmd
}

// handleSaveView drives viewSaveView: a single textinput naming the view
// "Save current view…" is about to create from currentViewSpecForSave().
// Modeled on handleCategoryManagerEdit - Escape cancels, Enter saves, any
// other key is forwarded to the input; there is nothing to navigate, so no
// NavUp/NavDown is needed. An empty name, an empty spec, or a name collision
// sets saveViewError instead of silently doing nothing, and leaves the
// prompt open so the user can fix it.
func (m model) handleSaveView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, m.keyMap.Escape):
		m.saveViewInput.Blur()
		m.saveViewInput.SetValue("")
		m.saveViewError = ""
		m.view = viewList
		return m, nil

	case key.Matches(msg, m.keyMap.Enter):
		name := strings.TrimSpace(m.saveViewInput.Value())
		if name == "" {
			m.saveViewError = "a name is required"
			return m, nil
		}

		spec := m.currentViewSpecForSave()
		// Scope alone doesn't count as a filter here - it is always
		// populated from wherever the user happens to be browsing, not a
		// deliberate choice the way a CLI "--scope" flag is - so it can't
		// by itself make an otherwise-empty save meaningful.
		if spec.Due == "" && len(spec.Statuses) == 0 && spec.Category == "" && !spec.IncludeDone {
			m.saveViewError = "nothing to save: no filter is active"
			return m, nil
		}

		if isBuiltInViewName(name) {
			m.saveViewError = fmt.Sprintf("%q is a built-in view name", name)
			return m, nil
		}

		if err := m.store.saveView(name, spec); err != nil {
			switch {
			case strings.Contains(err.Error(), "UNIQUE constraint failed"):
				m.saveViewError = fmt.Sprintf("a view named %q already exists", name)
			case errors.Is(err, errBuiltInViewName):
				m.saveViewError = fmt.Sprintf("%q is a built-in view name", name)
			default:
				m.saveViewError = err.Error()
			}
			return m, nil
		}

		if views, err := m.store.getSavedViews(); err == nil {
			m.savedViews = views
		}
		m.saveViewInput.Blur()
		m.saveViewInput.SetValue("")
		m.saveViewError = ""
		m.view = viewList
		return m, nil
	}

	m.saveViewInput, cmd = m.saveViewInput.Update(msg)
	return m, cmd
}

// handleCaptureInput drives viewCapture: a single textinput parsed live via
// ParseTask. m.captureEditing is nil when adding a new task, or points at
// the task being edited in place.
func (m model) handleCaptureInput(msg tea.KeyMsg, store *Store) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, m.keyMap.Escape):
		m.resetInputState()
		m.view = viewList
		return m, nil

	case m.captureEditing == nil && key.Matches(msg, m.keyMap.More):
		m.createMore = !m.createMore
		return m, nil

	case key.Matches(msg, m.keyMap.Enter):
		parsed := ParseTask(m.captureInput.Value(), time.Now())
		if parsed.Name == "" {
			// Refuse to save an empty task name - the preview line shows
			// "(no task name)" in this state.
			return m, nil
		}

		now := time.Now()
		var task Task

		if m.captureEditing == nil {
			task = Task{
				Name:      parsed.Name,
				Status:    parsed.Status,
				Completed: 0,
				CreatedAt: now,
				UpdatedAt: now,
			}
			// New tasks always target the project the app was launched
			// from - not the currently-viewed scope. When browsing All
			// projects the highlighted row's project is too easy to
			// misread, and filing a task into the wrong project silently
			// is worse than always filing it where you launched.
			if m.launchProject != nil {
				task.ProjectID = sql.NullInt64{Int64: m.launchProject.ID, Valid: true}
			}
		} else {
			existing := *m.captureEditing
			status := parsed.Status
			if status == "" {
				// "" means "leave default" - on an edit, default is
				// whatever the task's status already was, not "todo".
				status = existing.Status
			}
			task = Task{
				ID:          existing.ID,
				Name:        parsed.Name,
				Status:      status,
				Completed:   existing.Completed,
				CompletedAt: existing.CompletedAt,
				CreatedAt:   existing.CreatedAt,
				UpdatedAt:   now,
				ProjectID:   existing.ProjectID, // preserve the task's existing project unchanged
			}
		}

		task.Priority = parsed.Priority
		task.RecurRule = parsed.Recur

		if parsed.Category != "" {
			cat, err := store.getOrCreateCategory(parsed.Category)
			if err == nil && cat != nil {
				task.CategoryID = sql.NullInt64{Int64: cat.ID, Valid: true}
				task.CategoryName = cat.Name
				m.refreshCategories()
			}
		}

		if parsed.DueAt != nil {
			task.DueAt = sql.NullTime{Time: *parsed.DueAt, Valid: true}
			task.DueHasTime = parsed.DueHasTime
		}

		if err := store.saveTask(task); err == nil {
			m.refreshTasks()
		}

		if m.captureEditing == nil && m.createMore {
			m.captureInput.SetValue("")
			m.captureInput.Focus()
			return m, textinput.Blink
		}

		m.resetInputState()
		m.view = viewList
		return m, nil
	}

	m.captureInput, cmd = m.captureInput.Update(msg)
	return m, cmd
}

func (m model) handleFilterView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, m.keyMap.Escape):
		m.resetInputState()
		m.view = viewList
		return m, nil

	case key.Matches(msg, m.keyMap.NavUp):
		if m.categoryCursor > -1 {
			m.categoryCursor--
		}
		return m, nil

	case key.Matches(msg, m.keyMap.NavDown):
		if m.categoryCursor < len(m.filteredCategories)-1 {
			m.categoryCursor++
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Enter):
		if m.categoryCursor >= 0 && m.categoryCursor < len(m.filteredCategories) {
			m.filterCategory = &m.filteredCategories[m.categoryCursor]
			m.refreshTasks()
			m.cursor = -1
			if len(m.tasks) > 0 {
				m.cursor = 0
			}
		} else if m.filterInput.Value() == "" {
			// Clear filter
			m.filterCategory = nil
			m.refreshTasks()
			m.cursor = -1
			if len(m.tasks) > 0 {
				m.cursor = 0
			}
		}
		m.resetInputState()
		m.view = viewList
		return m, nil
	}

	m.filterInput, cmd = m.filterInput.Update(msg)
	m.updateFilteredCategories()
	return m, cmd
}

func (m model) handleCategoryManager(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keyMap.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keyMap.Escape):
		m.view = viewList
		return m, nil

	case key.Matches(msg, m.keyMap.Up):
		if m.categoryManagerCursor > 0 {
			m.categoryManagerCursor--
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Down):
		if m.categoryManagerCursor < len(m.categories)-1 {
			m.categoryManagerCursor++
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Add):
		m.view = viewCategoryManagerEdit
		m.editingCategory = nil
		m.categoryEditInput.SetValue("")
		m.categoryEditInput.Focus()
		return m, textinput.Blink

	case key.Matches(msg, m.keyMap.Edit):
		if len(m.categories) == 0 || m.categoryManagerCursor < 0 {
			return m, nil
		}
		cat := m.categories[m.categoryManagerCursor]
		m.view = viewCategoryManagerEdit
		m.editingCategory = &cat
		m.categoryEditInput.SetValue(cat.Name)
		m.categoryEditInput.Focus()
		return m, textinput.Blink

	case key.Matches(msg, m.keyMap.Delete):
		if len(m.categories) == 0 || m.categoryManagerCursor < 0 {
			return m, nil
		}
		catToDelete := m.categories[m.categoryManagerCursor]
		if err := m.store.deleteCategory(catToDelete.ID); err == nil {
			m.categoryUndoStack = append(m.categoryUndoStack, catToDelete)
			m.refreshCategories()
			if m.categoryManagerCursor >= len(m.categories) && len(m.categories) > 0 {
				m.categoryManagerCursor = len(m.categories) - 1
			} else if len(m.categories) == 0 {
				m.categoryManagerCursor = -1
			}
			m.refreshTasks()
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Undo):
		if len(m.categoryUndoStack) == 0 {
			return m, nil
		}
		catToRestore := m.categoryUndoStack[len(m.categoryUndoStack)-1]
		m.categoryUndoStack = m.categoryUndoStack[:len(m.categoryUndoStack)-1]
		if _, err := m.store.createCategory(catToRestore.Name); err == nil {
			m.refreshCategories()
			for i, c := range m.categories {
				if c.Name == catToRestore.Name {
					m.categoryManagerCursor = i
					break
				}
			}
		}
		return m, nil
	}

	return m, nil
}

func (m model) handleCategoryManagerEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, m.keyMap.Escape):
		m.categoryEditInput.Blur()
		m.categoryEditInput.SetValue("")
		m.editingCategory = nil
		m.view = viewCategoryManager
		return m, nil

	case key.Matches(msg, m.keyMap.Enter):
		name := m.categoryEditInput.Value()
		if name == "" {
			return m, nil
		}

		if m.editingCategory != nil {
			if err := m.store.updateCategory(m.editingCategory.ID, name); err == nil {
				m.refreshCategories()
				m.refreshTasks()
			}
		} else {
			if _, err := m.store.createCategory(name); err == nil {
				m.refreshCategories()
				m.categoryManagerCursor = len(m.categories) - 1
			}
		}

		m.categoryEditInput.Blur()
		m.categoryEditInput.SetValue("")
		m.editingCategory = nil
		m.view = viewCategoryManager
		return m, nil
	}

	m.categoryEditInput, cmd = m.categoryEditInput.Update(msg)
	return m, cmd
}

// handleProjectSwitcher handles the project switcher view (viewProjectSwitcher).
// The list is: "All projects", "Inbox", then every row from m.projects.
func (m model) handleProjectSwitcher(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keyMap.Escape):
		m.view = viewList
		return m, nil

	case key.Matches(msg, m.keyMap.Up):
		if m.projectSwitcherCursor > 0 {
			m.projectSwitcherCursor--
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Down):
		maxCursor := len(m.projects) + 1 // 0 = All projects, 1 = Inbox, 2.. = projects
		if m.projectSwitcherCursor < maxCursor {
			m.projectSwitcherCursor++
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Enter):
		switch m.projectSwitcherCursor {
		case 0:
			m.scope = scopeAllProjects
		case 1:
			m.scope = scopeInbox
			m.scopeProject = nil
		default:
			idx := m.projectSwitcherCursor - 2
			if idx >= 0 && idx < len(m.projects) {
				proj := m.projects[idx]
				m.scopeProject = &proj
				m.scope = scopeCurrentProject
			}
		}
		m.refreshTasks()
		m.cursor = -1
		if len(m.tasks) > 0 {
			m.cursor = 0
		}
		m.view = viewList
		return m, nil
	}

	return m, nil
}

func nextStatus(current string) string {
	switch current {
	case "todo":
		return "in-progress"
	case "in-progress":
		return "blocked"
	case "blocked":
		return "todo"
	default:
		return "todo"
	}
}

func main() {
	os.Exit(run(os.Args[1:]))
}

// runTUI launches the interactive Bubble Tea program - the default
// behaviour when tl is run with no subcommand. See cli.go for the
// headless "tl add/ls/done" commands and the run() dispatcher.
func runTUI() int {
	store := &Store{}
	if err := store.InitDb(); err != nil {
		fmt.Fprintf(os.Stderr, "Error in DB connection: %v\n", err)
		return 1
	}
	defer store.Close()

	cwd, _ := os.Getwd()
	root, _ := resolveProjectRoot(cwd)
	var proj *Project
	if root != nil {
		proj, _ = store.getOrCreateProject(root)
	}

	p := tea.NewProgram(initialModel(store, proj), tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Alas, there's been an error: %v\n", err)
		return 1
	}
	return 0
}
