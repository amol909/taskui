package main

import (
	"fmt"
	"os"
	"strings"
	"time"

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

	scopeHeaderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{
			Light: "#555555",
			Dark:  "#aaaaaa",
		}).
		Bold(true)

	projectBadgeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#666666"))

	statusTodoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	statusInProgressStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#36b8ff")).
				Bold(true)

	statusBlockedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ff6b6b")).
				Bold(true)

	dueBadgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{
			Light: "#888888",
			Dark:  "#aaaaaa",
		})

	overdueBadgeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ff6b6b")).
				Bold(true)

	priorityLowStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#888888"))

	priorityMedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ffb836"))

	priorityHighStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ff6b6b")).
				Bold(true)

	errorBannerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ff6b6b")).
				Bold(true).
				PaddingTop(1).
				PaddingBottom(1)
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
	case viewCapture:
		s.WriteString(m.renderCaptureInput())
	case viewFilter:
		s.WriteString(m.renderFilterInput())
	case viewCategoryManager:
		s.WriteString(m.renderCategoryManager())
	case viewCategoryManagerEdit:
		s.WriteString(m.renderCategoryManagerEdit())
	case viewProjectSwitcher:
		s.WriteString(m.renderProjectSwitcher())
	case viewAgenda:
		s.WriteString(m.renderAgenda())
	case viewPalette:
		s.WriteString(m.renderPalette())
	case viewSaveView:
		s.WriteString(m.renderSaveView())
	default:
		s.WriteString(m.renderTaskList())
	}

	return s.String()
}

// renderCaptureInput renders viewCapture: a single free-text box plus a live
// parse preview built from ParseTask, so the user sees what enter will save
// before they press it. Inline colouring of tokens inside the text field
// itself is deliberately out of scope - bubbles/textinput has no API for
// styled runs, and the preview line delivers the same feedback far more
// cheaply than reimplementing the widget.
func (m model) renderCaptureInput() string {
	var s strings.Builder

	title := "Add Task"
	if m.captureEditing != nil {
		title = "Edit Task"
	}
	s.WriteString(tasksHeadingStyle.Render(title) + "\n\n")

	s.WriteString(inputBorder.Render(m.captureInput.View()) + "\n")
	s.WriteString(m.renderCapturePreview() + "\n")

	if m.captureEditing == nil {
		modeLabel := "off"
		if m.createMore {
			modeLabel = "on"
		}
		s.WriteString("\n" + shortcutsStyle.Render("Create more: "+modeLabel+" • press tab to toggle") + "\n")
		s.WriteString(shortcutsStyle.Render("enter save • esc cancel • tab toggle create more") + "\n")
		return s.String()
	}

	s.WriteString("\n" + shortcutsStyle.Render("enter save • esc cancel") + "\n")

	return s.String()
}

// renderCapturePreview renders the "→ name  [category]  due ...  !priority"
// line below the capture box, e.g.:
//
//	→ pay rent          [finance]   due Fri 14 Aug 17:00   !high
//
// Only the parts ParseTask actually recognised are shown. It parses against
// the live clock (not a stored task) so the preview reflects exactly what
// pressing enter would save right now.
func (m model) renderCapturePreview() string {
	parsed := ParseTask(m.captureInput.Value(), time.Now())

	if parsed.Name == "" {
		return emptyStateStyle.Render("(no task name)")
	}

	line := "→ " + parsed.Name

	if parsed.Category != "" {
		line += "  " + categoryTagStyle.Render("["+parsed.Category+"]")
	}

	if parsed.DueAt != nil {
		text, overdue := formatDueBadge(*parsed.DueAt, parsed.DueHasTime, time.Now())
		style := dueBadgeStyle
		if overdue {
			style = overdueBadgeStyle
		}
		line += "  " + style.Render("due "+text)
	}

	if badge := formatPriorityBadge(parsed.Priority); badge != "" {
		line += "  " + badge
	}

	return line
}

// formatDueBadge renders a due date/time the way both the capture preview
// and the task list badge want it: "today" / "tomorrow" / "Fri 14 Aug" /
// "Fri 14 Aug 17:00", or "overdue" / "overdue Nd" for anything in the past.
// A date-only due date (hasTime false) is only overdue once the *end* of
// that day has passed - that is what due_has_time drives.
func formatDueBadge(due time.Time, hasTime bool, now time.Time) (text string, overdue bool) {
	today := dateOnly(now)
	dueDay := dateOnly(due)

	deadline := due
	if !hasTime {
		deadline = dueDay.AddDate(0, 0, 1) // end of the due day
	}

	if now.After(deadline) {
		days := int(dateOnly(now).Sub(dueDay).Hours() / 24)
		if days <= 0 {
			return "overdue", true
		}
		return fmt.Sprintf("overdue %dd", days), true
	}

	dayLabel := due.Format("Mon 2 Jan")
	switch {
	case dueDay.Equal(today):
		dayLabel = "today"
	case dueDay.Equal(today.AddDate(0, 0, 1)):
		dayLabel = "tomorrow"
	}

	if hasTime {
		return dayLabel + " " + due.Format("15:04"), false
	}
	return dayLabel, false
}

// formatPriorityBadge renders !/!!/!!! for low/medium/high priority in
// escalating colour, or "" for PriorityNone.
func formatPriorityBadge(priority int) string {
	switch priority {
	case PriorityLow:
		return priorityLowStyle.Render("!")
	case PriorityMed:
		return priorityMedStyle.Render("!!")
	case PriorityHigh:
		return priorityHighStyle.Render("!!!")
	}
	return ""
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

	// Scope header
	s.WriteString(m.renderScopeHeader())
	s.WriteString(m.renderViewBar())

	// Filter indicator
	if m.filterCategory != nil {
		s.WriteString(filterIndicatorStyle.Render(fmt.Sprintf("Filter: %s (%d tasks) • press esc to clear", m.filterCategory.Name, len(m.tasks))) + "\n")
	}

	s.WriteString(tasksHeadingStyle.Render("Tasks") + "\n")

	if m.loadErr != nil {
		s.WriteString(errorBannerStyle.Render(fmt.Sprintf("⚠ could not load tasks: %v  ·  your data has not been modified", m.loadErr)) + "\n")
	} else if len(m.tasks) == 0 {
		if m.filterCategory != nil {
			s.WriteString(emptyStateStyle.Render("No tasks in this category") + "\n")
		} else {
			s.WriteString(emptyStateStyle.Render("No tasks yet. Press 'a' to add one!") + "\n")
		}
	}

	// Calculate max category width for alignment
	maxCatWidth := m.calculateMaxCategoryWidth()

	// Calculate max project badge width for alignment, only needed when
	// browsing across every project.
	maxProjWidth := 0
	if m.scope == scopeAllProjects {
		maxProjWidth = m.calculateMaxProjectWidth()
	}

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

		line := fmt.Sprintf("%s%s ", cursor, checkbox)

		// Project badge, only shown when browsing across every project.
		if m.scope == scopeAllProjects {
			line += m.formatProjectBadge(task.ProjectName, maxProjWidth) + " "
		}

		// Category tag
		catTag := m.formatCategoryTag(task.CategoryName, maxCatWidth)

		// Task name
		taskName := task.Name
		if task.Completed == 1 {
			taskName = completedTaskStyle.Render(taskName)
		}

		line += fmt.Sprintf("%s %s", catTag, taskName)

		// Due badge: today / tomorrow / Fri 14 Aug [17:00], overdue in red.
		if task.DueAt.Valid {
			text, overdue := formatDueBadge(task.DueAt.Time, task.DueHasTime, time.Now())
			style := dueBadgeStyle
			if overdue {
				style = overdueBadgeStyle
			}
			line += " " + style.Render(text)
		}

		// Priority badge: ! / !! / !!! for low/medium/high, nothing for none.
		if badge := formatPriorityBadge(task.Priority); badge != "" {
			line += " " + badge
		}

		// Status badge
		line += " " + m.formatStatusBadge(task.Status)

		s.WriteString(listItemStyle.Render(line) + "\n")
	}

	// Help text
	helpText := "↑/↓ navigate • a add • e edit • d delete • enter toggle • s status • f filter • g agenda • ctrl+k commands • 1-9 views • c categories • A all projects • P switch project • q quit"
	if m.filterCategory != nil {
		helpText = "↑/↓ navigate • a add • e edit • d delete • enter toggle • s status • esc clear filter • g agenda • ctrl+k commands • 1-9 views • c categories • A all projects • P switch project • q quit"
	}
	s.WriteString("\n" + shortcutsStyle.Render(helpText) + "\n")

	return s.String()
}

// renderAgenda renders viewAgenda: the same task set as the list view
// (current scope + category filter, via m.tasks), grouped by bucketAgenda
// into Overdue / Today / Tomorrow / This week / Later / Someday headings.
// Empty buckets are skipped; the cursor is drawn against the flattened
// bucket order, matching handleAgendaView.
func (m model) renderAgenda() string {
	var s strings.Builder

	s.WriteString(m.renderScopeHeader())
	s.WriteString(m.renderViewBar())

	if m.filterCategory != nil {
		s.WriteString(filterIndicatorStyle.Render(fmt.Sprintf("Filter: %s (%d tasks) • press esc to clear", m.filterCategory.Name, len(m.tasks))) + "\n")
	}

	s.WriteString(tasksHeadingStyle.Render("Agenda") + "\n")

	if m.loadErr != nil {
		s.WriteString(errorBannerStyle.Render(fmt.Sprintf("⚠ could not load tasks: %v  ·  your data has not been modified", m.loadErr)) + "\n")
		s.WriteString("\n" + shortcutsStyle.Render("g/esc back to list • q quit") + "\n")
		return s.String()
	}

	buckets := bucketAgenda(m.tasks, time.Now())

	if len(buckets) == 0 {
		s.WriteString(emptyStateStyle.Render("No tasks yet. Press 'a' to add one!") + "\n")
	}

	maxCatWidth := m.calculateMaxCategoryWidth()

	idx := 0
	for _, b := range buckets {
		s.WriteString("\n" + scopeHeaderStyle.Render(fmt.Sprintf("%s (%d)", b.Label, len(b.Tasks))) + "\n")

		for _, task := range b.Tasks {
			cursor := "  "
			if m.cursor == idx {
				cursor = cursorStyle.Render("> ")
			}

			checkbox := "[ ]"
			if task.Completed == 1 {
				checkbox = "[✓]"
			}
			checkbox = checkboxStyle.Render(checkbox)

			line := fmt.Sprintf("%s%s ", cursor, checkbox)

			catTag := m.formatCategoryTag(task.CategoryName, maxCatWidth)

			taskName := task.Name
			if task.Completed == 1 {
				taskName = completedTaskStyle.Render(taskName)
			}

			line += fmt.Sprintf("%s %s", catTag, taskName)

			if task.DueAt.Valid {
				text, overdue := formatDueBadge(task.DueAt.Time, task.DueHasTime, time.Now())
				style := dueBadgeStyle
				if overdue {
					style = overdueBadgeStyle
				}
				line += " " + style.Render(text)
			}

			if badge := formatPriorityBadge(task.Priority); badge != "" {
				line += " " + badge
			}

			line += " " + m.formatStatusBadge(task.Status)

			s.WriteString(listItemStyle.Render(line) + "\n")
			idx++
		}
	}

	s.WriteString("\n" + shortcutsStyle.Render("↑/↓ navigate • enter toggle • d delete • s status • ctrl+k commands • 1-9 views • g/esc back to list • q quit") + "\n")

	return s.String()
}

// renderScopeHeader renders the current scope's header line above the task
// list, e.g.:
//
//	▸ taskui   ~/development/taskui            7 open · 2 done
//	▸ All projects                            43 open
//	▸ Inbox                                    5 open
func (m model) renderScopeHeader() string {
	var label, stats string

	switch m.scope {
	case scopeCurrentProject:
		if m.scopeProject == nil {
			return ""
		}
		id := m.scopeProject.ID
		open, done := m.scopeCounts(TaskQuery{ProjectID: &id})
		label = fmt.Sprintf("▸ %s   %s", m.scopeProject.Name, abbreviateHome(m.scopeProject.RootPath))
		stats = fmt.Sprintf("%d open · %d done", open, done)

	case scopeAllProjects:
		open, _ := m.scopeCounts(TaskQuery{})
		label = "▸ All projects"
		stats = fmt.Sprintf("%d open", open)

	case scopeInbox:
		open, _ := m.scopeCounts(TaskQuery{NoProject: true})
		label = "▸ Inbox"
		stats = fmt.Sprintf("%d open", open)

	default:
		return ""
	}

	return scopeHeaderStyle.Render(joinHeaderLine(label, stats, m.width)) + "\n"
}

// scopeCounts returns the open (incomplete) and done (completed, still
// visible under the default hide rule) task counts for a scope query.
func (m model) scopeCounts(q TaskQuery) (open int, done int) {
	tasks, err := m.store.findTasks(q)
	if err != nil {
		return 0, 0
	}
	for _, t := range tasks {
		if t.Completed == 1 {
			done++
		} else {
			open++
		}
	}
	return open, done
}

// abbreviateHome replaces the user's home directory prefix with "~".
func abbreviateHome(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// joinHeaderLine lays out a label and a right-aligned stats string on one
// line, truncating the label if the line would otherwise exceed width.
func joinHeaderLine(label, stats string, width int) string {
	if width <= 0 {
		width = 80
	}

	statsWidth := lipgloss.Width(stats)
	labelWidth := lipgloss.Width(label)

	if labelWidth+statsWidth+1 > width {
		maxLabelWidth := width - statsWidth - 1
		label = truncateToWidth(label, maxLabelWidth)
		labelWidth = lipgloss.Width(label)
	}

	gap := width - labelWidth - statsWidth
	if gap < 1 {
		gap = 1
	}

	return label + strings.Repeat(" ", gap) + stats
}

// truncateToWidth truncates s to at most maxWidth display columns
// (lipgloss.Width, not byte length), appending "…" when truncated.
func truncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}

	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > maxWidth {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
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

func (m model) calculateMaxProjectWidth() int {
	maxWidth := 0
	for _, task := range m.tasks {
		name := task.ProjectName
		if name == "" {
			name = "inbox"
		}
		if w := lipgloss.Width(name); w > maxWidth {
			maxWidth = w
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

func (m model) formatProjectBadge(projectName string, maxWidth int) string {
	displayName := projectName
	if displayName == "" {
		displayName = "inbox"
	}

	if lipgloss.Width(displayName) > maxWidth && maxWidth > 3 {
		displayName = truncateToWidth(displayName, maxWidth-2) + ".."
	}

	padding := ""
	if lipgloss.Width(displayName) < maxWidth {
		padding = strings.Repeat(" ", maxWidth-lipgloss.Width(displayName))
	}

	return projectBadgeStyle.Render("["+displayName+"]") + padding
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

// renderSaveView renders viewSaveView, opened by the palette's "Save
// current view…" command: a single name field, plus saveViewError inline
// when the last enter press was refused.
func (m model) renderSaveView() string {
	var s strings.Builder

	s.WriteString(tasksHeadingStyle.Render("Save Current View") + "\n\n")
	s.WriteString(inputLabelStyle.Render("View Name:") + "\n")
	s.WriteString(inputBorder.Render(m.saveViewInput.View()) + "\n")

	if m.saveViewError != "" {
		s.WriteString(errorBannerStyle.Render(m.saveViewError) + "\n")
	}

	s.WriteString("\n" + shortcutsStyle.Render("enter save • esc cancel") + "\n")

	return s.String()
}

func (m model) renderProjectSwitcher() string {
	var s strings.Builder

	s.WriteString(tasksHeadingStyle.Render("Switch Project") + "\n")

	fixedItems := []string{"All projects", "Inbox"}
	for idx, label := range fixedItems {
		cursor := "  "
		if m.projectSwitcherCursor == idx {
			cursor = cursorStyle.Render("> ")
		}
		line := fmt.Sprintf("%s%s", cursor, categoryTagStyle.Render(label))
		s.WriteString(listItemStyle.Render(line) + "\n")
	}

	for i, proj := range m.projects {
		idx := i + len(fixedItems)
		cursor := "  "
		if m.projectSwitcherCursor == idx {
			cursor = cursorStyle.Render("> ")
		}

		openCount, _ := m.store.getProjectTaskCount(proj.ID)
		countStr := fmt.Sprintf("(%d open)", openCount)

		line := fmt.Sprintf("%s%s %s", cursor, categoryTagStyle.Render(proj.Name), shortcutsStyle.Render(countStr))
		s.WriteString(listItemStyle.Render(line) + "\n")
	}

	s.WriteString("\n" + shortcutsStyle.Render("↑/↓ navigate • enter select • esc cancel") + "\n")

	return s.String()
}

// renderViewBar renders the numbered saved-views strip shown under the scope
// header in both renderTaskList and renderAgenda, e.g.:
//
//	1 Today · 2 Overdue · 3 Next 7 days · 4 Blocked
//
// The active view (if any) is highlighted. Skipped entirely when there are
// no saved views (in practice savedViews always carries the four built-ins,
// but an empty result - e.g. a store call that failed - is handled
// gracefully rather than assumed away). When the plain, unstyled line would
// already overflow m.width, per-item highlighting is dropped in favour of a
// single truncated line - truncateToWidth slices runes and would otherwise
// cut through the ANSI codes a styled render produces.
func (m model) renderViewBar() string {
	if len(m.savedViews) == 0 {
		return ""
	}

	labels := make([]string, len(m.savedViews))
	for i, v := range m.savedViews {
		labels[i] = fmt.Sprintf("%d %s", v.Position, v.Name)
	}
	plain := strings.Join(labels, " · ")

	width := m.width
	if width <= 0 {
		width = 80
	}

	if lipgloss.Width(plain) > width {
		return dropdownItemStyle.Render(truncateToWidth(plain, width)) + "\n"
	}

	parts := make([]string, len(m.savedViews))
	for i, v := range m.savedViews {
		style := dropdownItemStyle
		if m.activeView != nil && m.activeView.Position == v.Position {
			style = dropdownSelectedStyle
		}
		parts[i] = style.Render(labels[i])
	}

	return strings.Join(parts, dropdownItemStyle.Render(" · ")) + "\n"
}

// renderPalette renders viewPalette: the input in inputBorder at the top,
// then the ranked commands (rankCommands over commandsFor) grouped by Group
// in commandGroupOrder, best match first within a group. At most 8 results
// are shown, then a dim "... and N more" line, matching
// renderCategoryDropdown's idiom - including that idiom's quirk that
// paletteCursor can point past the visible rows without scrolling them into
// view (see handlePaletteView).
func (m model) renderPalette() string {
	var s strings.Builder

	s.WriteString(tasksHeadingStyle.Render("Command Palette") + "\n\n")
	s.WriteString(inputBorder.Render(m.paletteInput.View()) + "\n")

	cmds := rankCommands(commandsFor(&m), m.paletteInput.Value())

	if len(cmds) == 0 {
		s.WriteString(dropdownStyle.Render(emptyStateStyle.Render("no matching command")) + "\n")
		s.WriteString("\n" + shortcutsStyle.Render("↑/↓ navigate • enter run • esc close") + "\n")
		return s.String()
	}

	const maxItems = 8
	shown := cmds
	more := 0
	if len(cmds) > maxItems {
		shown = cmds[:maxItems]
		more = len(cmds) - maxItems
	}

	rowWidth := m.paletteInput.Width

	var items []string
	lastGroup := ""
	for idx, c := range shown {
		if c.Group != lastGroup {
			if lastGroup != "" {
				items = append(items, "")
			}
			items = append(items, dropdownItemStyle.Bold(true).Render(c.Group))
			lastGroup = c.Group
		}

		style := dropdownItemStyle
		prefix := "  "
		if idx == m.paletteCursor {
			style = dropdownSelectedStyle
			prefix = "> "
		}

		label := prefix + c.Title
		row := label
		if hint := keyHintFor(c.ID); hint != "" {
			row = joinHeaderLine(label, hint, rowWidth)
		}
		items = append(items, style.Render(row))
	}

	if more > 0 {
		items = append(items, dropdownItemStyle.Render(fmt.Sprintf("  ... and %d more", more)))
	}

	s.WriteString(dropdownStyle.Render(strings.Join(items, "\n")) + "\n")
	s.WriteString("\n" + shortcutsStyle.Render("↑/↓ navigate • enter run • esc close") + "\n")

	return s.String()
}
