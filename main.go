package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	viewList = iota
	viewAddCategory
	viewAddTask
	viewEditCategory
	viewEditTask
	viewFilter
)

type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Add    key.Binding
	Delete key.Binding
	Edit   key.Binding
	Filter key.Binding
	Enter  key.Binding
	Escape key.Binding
	Tab    key.Binding
	Quit   key.Binding
	Help   key.Binding
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Add, k.Edit, k.Filter, k.Enter, k.Escape},
		{k.Help, k.Quit},
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
	Add: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "add task"),
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
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "autocomplete"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

type model struct {
	view   int
	tasks  []Task
	cursor int
	store  *Store

	// Text inputs
	categoryInput textinput.Model
	taskInput     textinput.Model

	// Category autocomplete
	categories         []Category
	filteredCategories []Category
	categoryCursor     int
	selectedCategory   *Category

	// Filter state
	filterCategory   *Category
	filterInput      textinput.Model
	allTasks         []Task

	// Terminal size
	width  int
	height int

	keyMap KeyMap
	help   help.Model
}

func initialModel(store *Store) model {
	tasks, err := store.getAllTasks()
	if err != nil {
		log.Fatalf("Error getting tasks %v", err)
	}

	categories, _ := store.getAllCategories()

	catInput := textinput.New()
	catInput.Placeholder = "Category (empty for uncategorized)"
	catInput.CharLimit = 50
	catInput.Width = 40

	taskInput := textinput.New()
	taskInput.Placeholder = "Task name"
	taskInput.CharLimit = 156
	taskInput.Width = 60

	filterInput := textinput.New()
	filterInput.Placeholder = "Filter by category..."
	filterInput.CharLimit = 50
	filterInput.Width = 40

	return model{
		tasks:              tasks,
		allTasks:           tasks,
		view:               viewList,
		store:              store,
		categoryInput:      catInput,
		taskInput:          taskInput,
		filterInput:        filterInput,
		cursor:             -1,
		categories:         categories,
		filteredCategories: categories,
		categoryCursor:     -1,
		keyMap:             DefaultKeyMap,
		help:               help.New(),
		width:              80,
		height:             24,
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *model) refreshTasks() {
	if m.filterCategory != nil {
		tasks, err := m.store.getTasksByCategory(m.filterCategory.ID)
		if err == nil {
			m.tasks = tasks
		}
	} else {
		tasks, err := m.store.getAllTasks()
		if err == nil {
			m.tasks = tasks
			m.allTasks = tasks
		}
	}
}

func (m *model) refreshCategories() {
	categories, err := m.store.getAllCategories()
	if err == nil {
		m.categories = categories
	}
}

func (m *model) updateFilteredCategories() {
	query := m.categoryInput.Value()
	if m.view == viewFilter {
		query = m.filterInput.Value()
	}

	filtered, err := m.store.searchCategories(query)
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
	m.categoryInput.SetValue("")
	m.categoryInput.Blur()
	m.taskInput.SetValue("")
	m.taskInput.Blur()
	m.filterInput.SetValue("")
	m.filterInput.Blur()
	m.selectedCategory = nil
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

		case viewAddCategory, viewEditCategory:
			return m.handleCategoryInput(msg)

		case viewAddTask, viewEditTask:
			return m.handleTaskInput(msg, store)

		case viewFilter:
			return m.handleFilterView(msg)
		}
	}

	return m, cmd
}

func (m model) handleListView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keyMap.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keyMap.Add):
		m.view = viewAddCategory
		m.categoryInput.Focus()
		m.categoryInput.SetValue("")
		m.selectedCategory = nil
		m.categoryCursor = -1
		m.updateFilteredCategories()
		return m, textinput.Blink

	case key.Matches(msg, m.keyMap.Edit):
		if len(m.tasks) == 0 || m.cursor < 0 || m.cursor >= len(m.tasks) {
			return m, nil
		}
		task := m.tasks[m.cursor]
		m.view = viewEditCategory
		m.categoryInput.Focus()
		m.categoryInput.SetValue(task.CategoryName)
		if task.CategoryID.Valid {
			m.selectedCategory = &Category{ID: task.CategoryID.Int64, Name: task.CategoryName}
		} else {
			m.selectedCategory = nil
		}
		m.categoryCursor = -1
		m.updateFilteredCategories()
		return m, textinput.Blink

	case key.Matches(msg, m.keyMap.Filter):
		m.view = viewFilter
		m.filterInput.Focus()
		m.filterInput.SetValue("")
		m.categoryCursor = -1
		m.updateFilteredCategories()
		return m, textinput.Blink

	case key.Matches(msg, m.keyMap.Delete):
		if len(m.tasks) == 0 || m.cursor < 0 || m.cursor >= len(m.tasks) {
			return m, nil
		}
		taskToDelete := m.tasks[m.cursor]
		if err := m.store.deleteTask(taskToDelete); err == nil {
			m.refreshTasks()
			if m.cursor >= len(m.tasks) && len(m.tasks) > 0 {
				m.cursor = len(m.tasks) - 1
			} else if len(m.tasks) == 0 {
				m.cursor = -1
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
		newCompleted := 0
		if currentTask.Completed == 0 {
			newCompleted = 1
		}
		if err := m.store.updateTaskCompletion(currentTask.ID, newCompleted); err == nil {
			m.refreshTasks()
		}

	case key.Matches(msg, m.keyMap.Escape):
		// Clear filter
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

func (m model) handleCategoryInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, m.keyMap.Escape):
		m.resetInputState()
		m.view = viewList
		return m, nil

	case key.Matches(msg, m.keyMap.Up):
		if m.categoryCursor > 0 {
			m.categoryCursor--
		} else if m.categoryCursor == 0 {
			m.categoryCursor = -1
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Down):
		if m.categoryCursor < len(m.filteredCategories)-1 {
			m.categoryCursor++
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Tab):
		// Autocomplete: select highlighted category
		if m.categoryCursor >= 0 && m.categoryCursor < len(m.filteredCategories) {
			cat := m.filteredCategories[m.categoryCursor]
			m.categoryInput.SetValue(cat.Name)
			m.selectedCategory = &cat
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Enter):
		// Proceed to task input
		catName := m.categoryInput.Value()
		if catName != "" {
			// Use selected or create new
			if m.categoryCursor >= 0 && m.categoryCursor < len(m.filteredCategories) {
				cat := m.filteredCategories[m.categoryCursor]
				m.selectedCategory = &cat
			} else {
				cat, err := m.store.getOrCreateCategory(catName)
				if err == nil && cat != nil {
					m.selectedCategory = cat
					m.refreshCategories()
				}
			}
		} else {
			m.selectedCategory = nil
		}

		// Move to task input
		m.categoryInput.Blur()
		m.taskInput.Focus()

		if m.view == viewAddCategory {
			m.view = viewAddTask
			m.taskInput.SetValue("")
		} else {
			m.view = viewEditTask
			if m.cursor >= 0 && m.cursor < len(m.tasks) {
				m.taskInput.SetValue(m.tasks[m.cursor].Name)
			}
		}
		return m, textinput.Blink
	}

	// Update text input
	m.categoryInput, cmd = m.categoryInput.Update(msg)
	m.updateFilteredCategories()

	return m, cmd
}

func (m model) handleTaskInput(msg tea.KeyMsg, store *Store) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, m.keyMap.Escape):
		m.resetInputState()
		m.view = viewList
		return m, nil

	case key.Matches(msg, m.keyMap.Enter):
		taskName := m.taskInput.Value()
		if taskName == "" {
			return m, nil
		}

		now := time.Now()
		var task Task

		if m.view == viewAddTask {
			task = Task{
				ID:        0,
				Name:      taskName,
				DueDate:   "",
				Completed: 0,
				CreatedAt: now,
				UpdatedAt: now,
			}
		} else {
			// Edit mode
			if m.cursor < 0 || m.cursor >= len(m.tasks) {
				return m, nil
			}
			existing := m.tasks[m.cursor]
			task = Task{
				ID:        existing.ID,
				Name:      taskName,
				DueDate:   existing.DueDate,
				Completed: existing.Completed,
				CreatedAt: existing.CreatedAt,
				UpdatedAt: now,
			}
		}

		// Set category
		if m.selectedCategory != nil {
			task.CategoryID = sql.NullInt64{Int64: m.selectedCategory.ID, Valid: true}
			task.CategoryName = m.selectedCategory.Name
		}

		if err := store.saveTask(task); err == nil {
			m.refreshTasks()
			m.refreshCategories()
		}

		m.resetInputState()
		m.view = viewList
		return m, nil
	}

	m.taskInput, cmd = m.taskInput.Update(msg)
	return m, cmd
}

func (m model) handleFilterView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, m.keyMap.Escape):
		m.resetInputState()
		m.view = viewList
		return m, nil

	case key.Matches(msg, m.keyMap.Up):
		if m.categoryCursor > -1 {
			m.categoryCursor--
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Down):
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

func main() {
	store := &Store{}
	if err := store.InitDb(); err != nil {
		fmt.Printf("Error in DB connection: %v", err)
		os.Exit(1)
	}
	p := tea.NewProgram(initialModel(store), tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
