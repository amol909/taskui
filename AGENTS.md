# TaskUI

A terminal-based task management application built with Go and Bubble Tea.

## Tech Stack

- **Language**: Go 1.24
- **TUI Framework**: [Bubble Tea](https://github.com/charmbracelet/bubbletea) with [Bubbles](https://github.com/charmbracelet/bubbles) components
- **Styling**: [Lipgloss](https://github.com/charmbracelet/lipgloss)
- **Database**: SQLite3 (via `github.com/mattn/go-sqlite3`)

## Project Structure

- `main.go` - Application entry point, model definition, key bindings, and update logic
- `view.go` - View rendering and styling
- `store.go` - SQLite database operations and Task/Category structs

## Commands

```bash
# Build
go build -o taskui .

# Run
go run .

# Type check / verify
go build ./...
```

## Code Conventions

- Single package (`main`) application
- Bubble Tea Model-View-Update (MVU) architecture
- View modes (defined in main.go:16-25):
  - `viewList` (0) - main task list
  - `viewAddCategory` (1) - category input for new task
  - `viewAddTask` (2) - task input for new task
  - `viewEditCategory` (3) - category input for editing
  - `viewEditTask` (4) - task input for editing
  - `viewFilter` (5) - category filter selection
  - `viewCategoryManager` (6) - manage categories list
  - `viewCategoryManagerEdit` (7) - add/edit category
- Styles defined as package-level variables in view.go
- Store methods use pointer receivers except for read-only operations
- Time format for SQLite: `"2006-01-02 15:04:05"`

## Key Bindings

| Key | Action |
|-----|--------|
| `a` | Add new task |
| `e` | Edit selected task |
| `d` | Delete selected task |
| `enter` | Toggle completion / confirm |
| `j`/`↓` | Move down |
| `k`/`↑` | Move up |
| `f`/`/` | Filter by category |
| `c` | Manage categories |
| `u` | Undo delete (task or category) |
| `s` | Cycle task status (todo → in-progress → blocked) |
| `tab` | Autocomplete category / toggle create-more mode |
| `esc` | Cancel edit/add / clear filter / go back |
| `q`/`ctrl+c` | Quit |

## Database

- SQLite database stored at `~/.config/taskui/taskui.db` (user config dir)
- Tables: `categories` and `tasks` with foreign key relationship
- Auto-migration adds `category_id` column to existing databases
- Completed tasks older than 1 day are hidden from default queries

## Architecture Notes

- All state lives in `model` struct (main.go:110-147)
- No tests currently in the project
- Database uses upsert (`ON CONFLICT`) for task saves
- Categories are case-insensitive unique (COLLATE NOCASE)
- Undo stacks are session-only (in-memory, not persisted)
- Task status: `todo` (default), `in-progress`, `blocked` — cycled via `s` key, displayed as colored badge at end of task line