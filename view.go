package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	inputBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#36b8ff"))

	listItemStyle = lipgloss.NewStyle().
			PaddingTop(1).
			PaddingRight(2).
			PaddingBottom(1).
			PaddingLeft(2)

	greetingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{
			Light: "#36b8ff",
			Dark:  "#36b8ff",
		}).
		Background(lipgloss.AdaptiveColor{
			Light: "",
			Dark:  "",
		})

	tasksHeadingStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#36b8ff")).
				PaddingBottom(1).
				MarginTop(1)

	shortcutsStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{
			Light: "#888888",
			Dark:  "#666666",
		})

	cursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#36b8ff"))

	checkboxStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#36b8ff"))

	completedTaskStyle = lipgloss.NewStyle().
			Strikethrough(true)

	emptyStateStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{
				Light: "#888888",
				Dark:  "#666666",
			}).
			Italic(true).
			Align(lipgloss.Center).
			PaddingTop(2).
			PaddingBottom(1)
)

var (
	greetingText = `  _____  _    ____  _  ___   _ ___
 |_   _|/ \  / ___|| |/ / | | |_ _|
   | | / _ \ \___ \| ' /| | | || |
   | |/ ___ \ ___) | . \| |_| || |
   |_/_/   \_\____/|_|\_\\___/|___|

`
)

func (m model) View() string {
	s := "\n\n" + greetingStyle.Render(greetingText) + "\n\n"

	// Only show text input in add or edit modes (1 = edit, 2 = add)
	if m.view == 1 || m.view == 2 {
		s += "\n" + inputBorder.Render(m.textInput.View()) + "\n"
	}

	// Tasks heading
	s += tasksHeadingStyle.Render("Tasks") + "\n"

	if len(m.tasks) == 0 {
		emptyMessage := `🌙 Nothing on your plate yet! 

✨ Your task list is as empty as a zen garden ✨

Ready to fill it with greatness? Press 'a' to add your first task!`
		s += emptyStateStyle.Render(emptyMessage) + "\n"
	}

	for idx, task := range m.tasks {
		cursor := " "
		if m.cursor == idx {
			cursor = cursorStyle.Render(">")
		}

		checkbox := "[ ]"
		if task.Completed == 1 {
			checkbox = "[✓]"
		}
		checkbox = checkboxStyle.Render(checkbox)

		taskName := task.Name
		taskDueDate := task.DueDate
		if task.Completed == 1 {
			taskName = completedTaskStyle.Render(taskName)
			taskDueDate = completedTaskStyle.Render(taskDueDate)
		}

		s += listItemStyle.Render(fmt.Sprintf("%s %s %d. %s  %s", cursor, checkbox, idx+1, taskName, taskDueDate))
	}
	s += "\n" + shortcutsStyle.Render("Help: ↑/↓ - navigate | a - add a task | d - delete a task | enter - toggle completion | Press q to quit.") + "\n"

	return s
}
