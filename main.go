package main

import (
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
	list = iota
	edit
	add
)

type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Add    key.Binding
	Delete key.Binding
	Edit   key.Binding
	Enter  key.Binding
	Escape key.Binding
	Quit   key.Binding
	Help   key.Binding
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Add, k.Edit, k.Enter, k.Escape},
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
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

type model struct {
	view      int
	tasks     []Task
	cursor    int
	store     *Store
	textInput textinput.Model
	keyMap    KeyMap
	help      help.Model
}

func initialModel(store *Store) model {
	tasks, err := store.getAllTasks()
	tInput := textinput.New()
	tInput.Placeholder = "Enter task"
	tInput.CharLimit = 156
	tInput.Width = 80
	cursor := -1
	if err != nil {
		log.Fatalf("Error getting notes %v", err)
	}
	return model{
		tasks:     tasks,
		view:      list,
		store:     store,
		textInput: tInput,
		cursor:    cursor,
		keyMap:    DefaultKeyMap,
		help:      help.New(),
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	store := m.store
	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keyMap.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keyMap.Escape):
			if m.view == edit || m.view == add {
				m.view = list
				m.textInput.SetValue("")
				m.textInput.Blur()
			}
		case key.Matches(msg, m.keyMap.Add):
			if m.view == list {
				m.view = add
				m.textInput.Focus()
				m.textInput.SetValue("")
			}
		case key.Matches(msg, m.keyMap.Delete):
			// Only allow deletion in list mode with valid cursor
			if m.view != list {
				break
			}
			if len(m.tasks) == 0 || m.cursor < 0 || m.cursor >= len(m.tasks) {
				break
			}
			
			// Delete the task from database
			taskToDelete := m.tasks[m.cursor]
			if err := store.deleteTask(taskToDelete); err == nil {
				// Refresh task list after successful deletion
				if updatedTasks, err := store.getAllTasks(); err == nil {
					m.tasks = updatedTasks
					// Adjust cursor position if necessary
					if m.cursor >= len(m.tasks) && len(m.tasks) > 0 {
						m.cursor = len(m.tasks) - 1
					} else if len(m.tasks) == 0 {
						m.cursor = -1
					}
				}
			}
		case key.Matches(msg, m.keyMap.Edit):
			if m.view != list {
				break
			}
			if len(m.tasks) == 0 || m.cursor < 0 {
				break
			}
			m.view = edit
			m.textInput.SetValue(m.tasks[m.cursor].Name)
			m.textInput.Focus()
		case key.Matches(msg, m.keyMap.Up):
			if m.cursor >= 0 {
				m.cursor--
			}
		case key.Matches(msg, m.keyMap.Down):
			if m.cursor < len(m.tasks)-1 {
				m.cursor++
			}
		case key.Matches(msg, m.keyMap.Enter):
			// Handle enter key based on current view mode
			if m.view == list {
				// In list mode: toggle task completion
				if len(m.tasks) == 0 || m.cursor < 0 || m.cursor >= len(m.tasks) {
					break
				}
				currentTask := m.tasks[m.cursor]
				newCompleted := 0
				if currentTask.Completed == 0 {
					newCompleted = 1
				}
				
				if err := store.updateTaskCompletion(currentTask.ID, newCompleted); err == nil {
					// Refresh task list after successful update
					if updatedTasks, err := store.getAllTasks(); err == nil {
						m.tasks = updatedTasks
					}
				}
			} else if m.view != add && m.view != edit {
				break
			}

			value := m.textInput.Value()
			if value == "" {
				break
			}

			var task Task
			if m.view == edit {
				if m.cursor >= 0 && m.cursor < len(m.tasks) {
					task = Task{
						ID:        m.tasks[m.cursor].ID,
						Name:      value,
						DueDate:   m.tasks[m.cursor].DueDate,
						Completed: m.tasks[m.cursor].Completed,
						CreatedAt: m.tasks[m.cursor].CreatedAt,
						UpdatedAt: time.Now(),
					}
				}
			} else if m.view == add {
				// Add mode: create new task
				now := time.Now()
				task = Task{
					ID:        0,
					Name:      value,
					DueDate:   "",
					Completed: 0,
					CreatedAt: now,
					UpdatedAt: now,
				}
			}

			if err := store.saveTask(task); err == nil {
				// Refresh task list after successful save
				if updatedTasks, err := store.getAllTasks(); err == nil {
					m.tasks = updatedTasks
				}
				// Clear text input and return to list view
				m.textInput.SetValue("")
				m.textInput.Blur()
				m.view = list
			}
		}
	}
	
	// Only update textInput when we're in add or edit mode
	// This prevents mode-switching keys ('a', 'e') from being added to the input
	if m.view == add || m.view == edit {
		m.textInput, cmd = m.textInput.Update(msg)
	}

	return m, cmd

}

func main() {
	store := &Store{}
	if err := store.InitDb(); err != nil {
		fmt.Printf("Error in DB connection: %v", err)
		os.Exit(1)
	}
	p := tea.NewProgram(initialModel(store))

	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
