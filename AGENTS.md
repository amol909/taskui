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
- `store.go` - SQLite database operations and Task struct

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
- View modes: `list` (0), `edit` (1), `add` (2)
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
| `esc` | Cancel edit/add |
| `q`/`ctrl+c` | Quit |
