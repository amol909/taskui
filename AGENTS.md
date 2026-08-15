# TaskUI

A terminal-based task management application built with Go and Bubble Tea, plus `tl`, a headless CLI over the same SQLite database.

## Tech Stack

- **Language**: Go 1.24
- **TUI Framework**: [Bubble Tea](https://github.com/charmbracelet/bubbletea) with [Bubbles](https://github.com/charmbracelet/bubbles) components
- **Styling**: [Lipgloss](https://github.com/charmbracelet/lipgloss)
- **Database**: SQLite3 (via `github.com/mattn/go-sqlite3`)

## Project Structure

- `main.go` - TUI entry point (`main`/`run` dispatch to `runTUI` or the CLI), model definition, key bindings, and update logic
- `view.go` - View rendering and styling, including the due-date/priority badge helpers shared with `cli.go`
- `store.go` - SQLite database operations and the `Task`/`Category`/`Project` structs
- `query.go` - `TaskQuery` and `buildTaskQuery`: the single filter language every task listing (TUI and CLI) goes through
- `project.go` - `resolveProjectRoot`: walks up from a directory to find the enclosing project (`.taskui-root` / `.git` / language marker)
- `parse.go` - `ParseTask`/`FormatTask`: the natural-language capture grammar (see below)
- `recur.go` - `RecurRule`, `ParseRecur`: the recurrence grammar (`every day`, `every other tuesday`, `last day of month`, ...). `Next(after)` computes the occurrence after a *completed* one; `First(now)` computes the first occurrence of a newly created recurring task - anchored rules (weekday, last-day-of-month) land on the next point of their grid rather than a full interval out, unanchored ones (`every N days`, `every month`, `every year`) fall back to `Next`. `parse.go`'s due-date fallback uses `First`; `completeTask` (store.go) uses `Next`.
- `agenda.go` - `bucketAgenda`/`dueBucketOf`: the agenda's Overdue/Today/Tomorrow/This week/Later/Someday buckets. `dueBucketOf` is the single predicate every due-based surface shares - the agenda view, `tl ls --due`, and `resolveViewSpec`'s `Due` field all agree on what "overdue" means (end-of-day for a date-only task, the exact instant for a timed one) because they go through it rather than each reimplementing midnight-boundary math. Bucket semantics live here and nowhere else: `resolveViewSpec` and `cli.go`'s `--due` both populate `TaskQuery.DueBuckets` (via `dueRangeLabels`) instead of relying on `DueAfter`/`DueBefore` alone, and `findTasks` (store.go) is the one place that applies `filterByDueBucket` against it - so `tl ls --due today`, `tl ls --view today`/the `1` key, and the agenda cannot drift apart the way they once did (a date-only bound can't express "the deadline is the end of the day", only `dueBucketOf` can).
- `view_spec.go` - `ViewSpec`/`SavedView`/`resolveViewSpec`: saved, named filters (see Saved views below).
- `palette.go` - `Command`/`commandsFor`/`fuzzyMatch`/`rankCommands`: the command palette's registry and fuzzy matcher (see Command palette below).
- `cli.go` - the headless `tl add|ls|done|views` commands and the `run()` dispatcher. `tl views add <name> [flags]`/`tl views rm <name>` create/delete user-defined saved views (see Saved views below).

## Commands

```bash
# Build
go build -o taskui .          # TUI binary
make build                    # ./tl (see Makefile: build/install/test/fmt)
make install                  # installs tl to $GOBIN

# Run
go run .

# Type check / verify
go build ./...
go vet ./...
gofmt -l .
go test ./...
```

## Code Conventions

- Single package (`main`) application
- Bubble Tea Model-View-Update (MVU) architecture
- View modes (defined in main.go):
  - `viewList` (0) - main task list
  - `viewCapture` (1) - single-box quick-capture add/edit (see Capture below)
  - `viewFilter` (2) - category filter selection
  - `viewCategoryManager` (3) - manage categories list
  - `viewCategoryManagerEdit` (4) - add/edit category
  - `viewProjectSwitcher` (5) - switch scope / project
  - `viewAgenda` (6) - tasks grouped into Overdue/Today/Tomorrow/This week/Later/Someday buckets
  - `viewPalette` (7) - the ctrl+k command palette
  - `viewSaveView` (8) - name prompt for the palette's "Save current view…" command
- Styles defined as package-level variables in view.go
- Store methods use pointer receivers except for read-only operations
- Time format for SQLite: `"2006-01-02 15:04:05"`, **always local time** - never bare `datetime('now')` in SQL (it's UTC); compute date boundaries in Go and bind them as parameters, or use `datetime('now','localtime', ...)`
- **`parse.go` must never call `time.Now()` internally.** `ParseTask(input, now)` always takes `now` as a parameter - that is what keeps its test suite deterministic. Callers (main.go, cli.go) pass `time.Now()` at the call site.

## Quick capture (`parse.go`)

`a` in the TUI (and `tl add`) both go through `ParseTask`, which pulls
`#category`, `!priority`, `@status` and a due date/time out of a free-text
line, leaving everything else as the task name. `FormatTask` is the inverse,
used to pre-fill the edit box; `ParseTask(FormatTask(t), now)` round-trips
Category, Priority, Status, DueAt and DueHasTime - see the round-trip test
in `parse_test.go` if you touch either function. Full grammar and examples
are in `readme.md`.

## Key Bindings

| Key | Action |
|-----|--------|
| `a` | Quick capture: add a task |
| `e` | Edit selected task (pre-filled via `FormatTask`) |
| `d` | Delete selected task |
| `enter` | Toggle completion / confirm |
| `j`/`↓` | Move down |
| `k`/`↑` | Move up |
| `f`/`/` | Filter by category |
| `c` | Manage categories |
| `u` | Undo delete (task or category) |
| `s` | Cycle task status (todo → in-progress → blocked) |
| `A` | Toggle all-projects scope |
| `P` | Switch project |
| `g` | Agenda view |
| `ctrl+k` | Open the command palette |
| `1`-`9` | Activate the saved view at that position |
| `tab` | Toggle create-more mode (in the capture box) |
| `esc` | Cancel edit/add / clear an active saved view, then the category filter / go back |
| `q`/`ctrl+c` | Quit |

## Saved views (`view_spec.go`, `store.go`)

A `SavedView` pairs a name with a `ViewSpec` - a small, JSON-serialisable
filter (scope, due range, statuses, category, include-done) that
`resolveViewSpec` turns into a `TaskQuery` the normal `findTasks` path
already knows how to run. Four built-ins - **Today**, **Overdue**, **Next 7
days**, **Blocked** - always occupy positions 1-4.

**Built-ins live in code (`builtInViews` in view_spec.go), not the `views`
table**, deliberately: the `1`-`9` keybindings and the palette's "View: ..."
commands assume position 1 is always Today, position 4 is always Blocked,
and so on. If built-ins were ordinary rows a user could rename or delete
them and silently break that assumption (`1` now opens something else, or a
key press does nothing). Keeping them as a Go slice makes that impossible -
`getSavedViews()` always prepends them before any user-defined rows from the
`views` table and renumbers positions 1..N over the combined list. Only
user-created views are ever persisted.

The TUI (`model.savedViews`/`activeView`/`activateSavedView`/
`clearSavedView` in main.go), the palette (one `Command` per view, ID
`view-<position>`) and the CLI (`tl views`, `tl ls --view <name>`) all read
the same `getSavedViews()`/`resolveViewSpec` pair rather than each having
their own notion of what a view is.

**Creating/deleting user views.** `store.saveView`/`store.deleteView` are the
only place a `views` row is written or removed:

- CLI: `tl views add <name> [--due today|tomorrow|week|overdue] [--status <s>]... [--category <c>] [--scope project|all|inbox] [--include-done]` (at least one flag is required - an empty spec means nothing) and `tl views rm <name>`.
- TUI: the palette's **"Save current view…"** command (Group `View`) opens `viewSaveView`, a single name prompt (modeled on `viewCategoryManagerEdit` - `NavUp`/`NavDown` are not needed since there is nothing to navigate). It captures `m.currentViewSpecForSave()`: `Scope` from `m.scope`, `Category` from `m.filterCategory` when set, `Due`/`Statuses` from `m.activeView.Spec` when a view is active - so "Today, narrowed to #work" saves as exactly that. The palette also offers one **"Delete view: `<name>`"** command per *user-defined* view (never built-ins) - the palette is the deletion UI, there is no separate one.
- Both layers reject a name colliding (case-insensitively) with a built-in via the shared `isBuiltInViewName`/`findBuiltInView` (view_spec.go) *before* touching the store, and `store.saveView` enforces the same rule itself (wrapping `errBuiltInViewName`) as the actual point of truth, the same way the `UNIQUE COLLATE NOCASE` column enforces duplicate user names - so there is exactly one thing that can happen if a caller forgets to check first.
- Either layer refreshes `m.savedViews`/re-reads `getSavedViews()` after a save or delete so the view bar and `1`-`9` keys update immediately.

## Command palette (`palette.go`)

`ctrl+k` opens `viewPalette`: a `textinput` plus `commandsFor(m)`, the full
list of actions available given the current model state (task-specific
commands like "Delete task" only appear with a task selected), ranked by
`rankCommands` against the typed text via `fuzzyMatch` (case-insensitive
subsequence matching with bonuses for consecutive runs and word-boundary
starts). Each `Command.Run(*model)` mutates the model exactly like a
key-handler in main.go would, then returns `*m` as the `tea.Model` -
`handlePaletteView` closes the palette back to whichever view opened it
(`paletteReturnView`) only when `Run` didn't already navigate somewhere
itself (e.g. "Add task" opens `viewCapture` directly; activating a saved
view does not change `m.view`, so the palette closes on its own).
`keyHintFor(id)` looks up the existing keybinding a command duplicates, so
the palette teaches the shortcuts rather than replacing them.

## Database

- SQLite database stored at `~/.config/taskui/taskui.db` (user config dir)
- Tables: `categories`, `tasks`, `projects`
- `migrate()` in store.go applies best-effort `ALTER TABLE` migrations for existing databases (category_id, status, completed_at, project_id, due_at, due_has_time, priority) and drops the legacy `update_tasks_updated_at` trigger (see below)
- `tasks.due_date TEXT` is **vestigial** - it predates `due_at`/`due_has_time`, has only ever been written as `""`, and is no longer read or written anywhere. It is left in the schema (dropping columns breaks older SQLite) but the `Task` struct has no `DueDate` field.
- `tasks.due_at DATETIME` / `tasks.due_has_time INTEGER` hold the real due date: `due_at` is local time in the usual `"2006-01-02 15:04:05"` format (midnight when only a date was given); `due_has_time` drives display (`Fri 14 Aug` vs `Fri 14 Aug 17:00`) and end-of-day overdue semantics
- `tasks.priority INTEGER` - 0 none, 1 low, 2 medium, 3 high (`PriorityNone`/`PriorityLow`/`PriorityMed`/`PriorityHigh` in store.go)
- Legacy databases may carry a `update_tasks_updated_at` trigger that rewrote `updated_at` using `CURRENT_TIMESTAMP` (UTC) on every update, corrupting it relative to the rest of the local-time columns; `migrate()` drops it unconditionally
- Completed tasks older than 1 day are hidden from default queries (`TaskQuery.IncludeDone` opts back in)
- All task listing - TUI and CLI alike - goes through `TaskQuery`/`buildTaskQuery`/`findTasks` (query.go). Extending what a listing can filter on means adding a field to `TaskQuery` and handling it in `buildTaskQuery`, not writing ad hoc SQL at the call site.

## Architecture Notes

- All state lives in the `model` struct (main.go)
- Database uses upsert (`ON CONFLICT`) for project saves; tasks go through `insertTask`/`updateTask`/`restoreTask` (store.go), all of which carry `due_at`/`due_has_time`/`priority` alongside the older fields
- Categories are case-insensitive unique (COLLATE NOCASE)
- Undo stacks are session-only (in-memory, not persisted)
- Task status: `todo` (default), `in-progress`, `blocked` — cycled via `s` key, displayed as a colored badge at the end of the task line
- Projects are resolved once at startup via `resolveProjectRoot` + `getOrCreateProject`; new tasks always target `model.launchProject` (the project the app was launched from), never the currently-*viewed* scope - see the comment at the capture save path in main.go
- `newTestStore(t)` (store_test.go) opens an in-memory (`:memory:`) SQLite store for tests
