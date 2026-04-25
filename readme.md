# TaskUI

A fast, keyboard-first terminal task manager built with Go, Bubble Tea, and SQLite.

<p align="center">
  <img src="assets/taskui-screenshot.png" alt="TaskUI screenshot" width="1200" />
</p>

## What it does

TaskUI helps you manage personal tasks directly from the terminal with a clean TUI experience. You can organize tasks by category, quickly add or edit items, filter what you see, and keep everything stored locally in SQLite.

## Features

- **Terminal-first workflow** with a polished Bubble Tea interface
- **Persistent storage** using SQLite
- **Task categories** to group related work
- **Add, edit, delete, and toggle completion** for tasks
- **Filter tasks** to focus on a specific category or subset
- **Category manager** for creating, renaming, and deleting categories
- **Undo delete** support for tasks and categories during the current session
- **Create-more mode** for rapidly adding multiple tasks in a row
- **Keyboard-driven navigation** for a fast, mouse-free workflow

## Key bindings

- `a` — add task
- `e` — edit selected task
- `d` — delete selected task
- `enter` — toggle completion / confirm
- `j` / `↓` — move down
- `k` / `↑` — move up
- `f` — filter tasks
- `c` — manage categories
- `u` — undo delete
- `esc` — cancel / go back
- `q` / `ctrl+c` — quit

## Build and run

```bash
go build -o taskui .
./taskui
```

Or run directly:

```bash
go run .
```

## Notes

- Task data is stored locally in your user config directory as `taskui/taskui.db`.
- The app is built as a single-package Go application using Bubble Tea, Bubbles, Lip Gloss, and SQLite.
