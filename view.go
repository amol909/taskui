package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	inputBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#36b8ff")).
			Padding(0, 1)

	inputLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#36b8ff")).
			Bold(true)

	listItemStyle = lipgloss.NewStyle().
			PaddingTop(0).
			PaddingRight(2).
			PaddingBottom(0).
			PaddingLeft(2)

	greetingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{
			Light: "#36b8ff",
			Dark:  "#36b8ff",
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
				Strikethrough(true).
				Foreground(lipgloss.Color("#666666"))

	categoryTagStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ffb836")).
				Bold(true)

	emptyStateStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{
				Light: "#888888",
				Dark:  "#666666",
			}).
			Italic(true).
			PaddingTop(1).
			PaddingBottom(1)

	dropdownStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#444444")).
			Padding(0, 1).
			MarginLeft(2)

	dropdownItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#aaaaaa"))

	dropdownSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#36b8ff")).
				Bold(true)

	filterIndicatorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ffb836")).
				Bold(true).
				Padding(0, 1).
				MarginBottom(1)

	statusTodoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	statusInProgressStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#36b8ff")).
			Bold(true)

	statusBlockedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff6b6b")).
			Bold(true)
)

var greetingText = `  _____  _    ____  _  ___   _ ___
 |_   _|/ \  / ___|| |/ / | | |_ _|
   | | / _ \ \___ \| ' /| | | || |
   | |/ ___ \ ___) | . \| |_| || |
   |_/_/   \_\____/|_|\_\\___/|___|
`

func (m model) View() string {
	var s strings.Builder

	s.WriteString("\n" + greetingStyle.Render(greetingText) + "\n\n")

	// Render based on current view
	switch m.view {
	case viewAddCategory, viewEditCategory:
		s.WriteString(m.renderCategoryInput())
	case viewAddTask, viewEditTask:
		s.WriteString(m.renderTaskInput())
	case viewFilter:
		s.WriteString(m.renderFilterInput())
	case viewCategoryManager:
		s.WriteString(m.renderCategoryManager())
	case viewCategoryManagerEdit:
		s.WriteString(m.renderCategoryManagerEdit())
	default:
		s.WriteString(m.renderTaskList())
	}

	return s.String()
}

func (m model) renderCategoryInput() string {
	var s strings.Builder

	title := "Add Task"
	if m.view == viewEditCategory {
		title = "Edit Task"
	}
	s.WriteString(tasksHeadingStyle.Render(title) + "\n\n")

	s.WriteString(inputLabelStyle.Render("Category:") + "\n")
	s.WriteString(inputBorder.Render(m.categoryInput.View()) + "\n")

	// Render category dropdown
	if len(m.filteredCategories) > 0 {
		s.WriteString(m.renderCategoryDropdown())
	} else if m.categoryInput.Value() != "" {
		s.WriteString(dropdownStyle.Render("(new category will be created)") + "\n")
	}

	s.WriteString("\n" + shortcutsStyle.Render("↑/↓ navigate • tab autocomplete • enter confirm • esc cancel") + "\n")

	return s.String()
}

func (m model) renderTaskInput() string {
	var s strings.Builder

	title := "Add Task"
	if m.view == viewEditTask {
		title = "Edit Task"
	}
	s.WriteString(tasksHeadingStyle.Render(title) + "\n\n")

	// Show selected category
	catDisplay := "(uncategorized)"
	if m.selectedCategory != nil {
		catDisplay = categoryTagStyle.Render("[" + m.selectedCategory.Name + "]")
	}
	s.WriteString(inputLabelStyle.Render("Category: ") + catDisplay + "\n\n")

	s.WriteString(inputLabelStyle.Render("Task:") + "\n")
	s.WriteString(inputBorder.Render(m.taskInput.View()) + "\n")

	if m.view == viewAddTask {
		modeLabel := "off"
		if m.createMore {
			modeLabel = "on"
		}
		s.WriteString("\n" + shortcutsStyle.Render("Create more: "+modeLabel+" • press tab to toggle") + "\n")
		s.WriteString(shortcutsStyle.Render("enter save • esc exit • tab toggle create more") + "\n")
		return s.String()
	}

	s.WriteString("\n" + shortcutsStyle.Render("enter save • esc cancel") + "\n")

	return s.String()
}

func (m model) renderFilterInput() string {
	var s strings.Builder

	s.WriteString(tasksHeadingStyle.Render("Filter by Category") + "\n\n")
	s.WriteString(inputBorder.Render(m.filterInput.View()) + "\n")

	// Show "All tasks" option
	allStyle := dropdownItemStyle
	if m.categoryCursor == -1 {
		allStyle = dropdownSelectedStyle
	}
	s.WriteString(dropdownStyle.Render(allStyle.Render("All tasks (clear filter)")) + "\n")

	// Render category dropdown
	if len(m.filteredCategories) > 0 {
		s.WriteString(m.renderCategoryDropdown())
	}

	s.WriteString("\n" + shortcutsStyle.Render("↑/↓ navigate • enter select • esc cancel") + "\n")

	return s.String()
}

func (m model) renderCategoryDropdown() string {
	var items []string

	maxItems := 5
	if len(m.filteredCategories) < maxItems {
		maxItems = len(m.filteredCategories)
	}

	for i := 0; i < maxItems; i++ {
		cat := m.filteredCategories[i]
		style := dropdownItemStyle
		prefix := "  "
		if i == m.categoryCursor {
			style = dropdownSelectedStyle
			prefix = "> "
		}
		items = append(items, style.Render(prefix+cat.Name))
	}

	if len(m.filteredCategories) > maxItems {
		items = append(items, dropdownItemStyle.Render(fmt.Sprintf("  ... and %d more", len(m.filteredCategories)-maxItems)))
	}

	return dropdownStyle.Render(strings.Join(items, "\n")) + "\n"
}

func (m model) renderTaskList() string {
	var s strings.Builder

	// Filter indicator
	if m.filterCategory != nil {
		s.WriteString(filterIndicatorStyle.Render(fmt.Sprintf("Filter: %s (%d tasks) • press esc to clear", m.filterCategory.Name, len(m.tasks))) + "\n")
	}

	s.WriteString(tasksHeadingStyle.Render("Tasks") + "\n")

	if len(m.tasks) == 0 {
		if m.filterCategory != nil {
			s.WriteString(emptyStateStyle.Render("No tasks in this category") + "\n")
		} else {
			s.WriteString(emptyStateStyle.Render("No tasks yet. Press 'a' to add one!") + "\n")
		}
	}

	// Calculate max category width for alignment
	maxCatWidth := m.calculateMaxCategoryWidth()

	for idx, task := range m.tasks {
		cursor := "  "
		if m.cursor == idx {
			cursor = cursorStyle.Render("> ")
		}

		checkbox := "[ ]"
		if task.Completed == 1 {
			checkbox = "[✓]"
		}
		checkbox = checkboxStyle.Render(checkbox)

		// Category tag
		catTag := m.formatCategoryTag(task.CategoryName, maxCatWidth)

		// Task name
		taskName := task.Name
		if task.Completed == 1 {
			taskName = completedTaskStyle.Render(taskName)
		}

		// Status badge
		statusBadge := m.formatStatusBadge(task.Status)

		line := fmt.Sprintf("%s%s %s %s %s", cursor, checkbox, catTag, taskName, statusBadge)
		s.WriteString(listItemStyle.Render(line) + "\n")
	}

	// Help text
	helpText := "↑/↓ navigate • a add • e edit • d delete • enter toggle • s status • f filter • c categories • q quit"
	if m.filterCategory != nil {
		helpText = "↑/↓ navigate • a add • e edit • d delete • enter toggle • s status • esc clear filter • c categories • q quit"
	}
	s.WriteString("\n" + shortcutsStyle.Render(helpText) + "\n")

	return s.String()
}

func (m model) calculateMaxCategoryWidth() int {
	maxWidth := 0
	for _, task := range m.tasks {
		if len(task.CategoryName) > maxWidth {
			maxWidth = len(task.CategoryName)
		}
	}
	// Cap at reasonable width for responsiveness
	termWidth := m.width
	if termWidth == 0 {
		termWidth = 80
	}
	maxAllowed := termWidth / 4
	if maxWidth > maxAllowed {
		maxWidth = maxAllowed
	}
	return maxWidth
}

func (m model) formatCategoryTag(catName string, maxWidth int) string {
	if catName == "" {
		// Pad to align with other categories
		if maxWidth > 0 {
			return strings.Repeat(" ", maxWidth+2) // +2 for brackets
		}
		return ""
	}

	// Truncate if needed
	displayName := catName
	if len(displayName) > maxWidth && maxWidth > 3 {
		displayName = displayName[:maxWidth-2] + ".."
	}

	// Pad for alignment
	padding := ""
	if len(displayName) < maxWidth {
		padding = strings.Repeat(" ", maxWidth-len(displayName))
	}

	return categoryTagStyle.Render("["+displayName+"]") + padding
}

func (m model) formatStatusBadge(status string) string {
	switch status {
	case "in-progress":
		return statusInProgressStyle.Render("[in-progress]")
	case "blocked":
		return statusBlockedStyle.Render("[blocked]")
	default:
		return statusTodoStyle.Render("[todo]")
	}
}

func (m model) renderCategoryManager() string {
	var s strings.Builder

	s.WriteString(tasksHeadingStyle.Render("Manage Categories") + "\n")

	if len(m.categories) == 0 {
		s.WriteString(emptyStateStyle.Render("No categories yet. Press 'a' to add one!") + "\n")
	} else {
		for idx, cat := range m.categories {
			cursor := "  "
			if m.categoryManagerCursor == idx {
				cursor = cursorStyle.Render("> ")
			}

			taskCount, _ := m.store.getTaskCountByCategory(cat.ID)
			countStr := fmt.Sprintf("(%d tasks)", taskCount)

			line := fmt.Sprintf("%s%s %s", cursor, categoryTagStyle.Render(cat.Name), shortcutsStyle.Render(countStr))
			s.WriteString(listItemStyle.Render(line) + "\n")
		}
	}

	s.WriteString("\n" + shortcutsStyle.Render("↑/↓ navigate • a add • e edit • d delete • u undo • esc back • q quit") + "\n")

	return s.String()
}

func (m model) renderCategoryManagerEdit() string {
	var s strings.Builder

	title := "Add Category"
	if m.editingCategory != nil {
		title = "Edit Category"
	}
	s.WriteString(tasksHeadingStyle.Render(title) + "\n\n")

	s.WriteString(inputLabelStyle.Render("Category Name:") + "\n")
	s.WriteString(inputBorder.Render(m.categoryEditInput.View()) + "\n")

	s.WriteString("\n" + shortcutsStyle.Render("enter save • esc cancel") + "\n")

	return s.String()
}
