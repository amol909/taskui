package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const appVersion = "0.2.0"

// run is the single entrypoint for both the TUI and the headless CLI
// commands. main() just calls os.Exit(run(os.Args[1:])) - keeping it a
// plain function (rather than inlined in main) makes it callable from
// tests without spawning a process. No args launches the TUI, matching
// tl's previous always-TUI behaviour.
func run(args []string) int {
	if len(args) == 0 {
		return runTUI()
	}

	switch args[0] {
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		return 0
	case "version", "--version":
		fmt.Println("tl version " + appVersion)
		return 0
	case "add":
		return cliAdd(args[1:])
	case "ls":
		return cliLs(args[1:])
	case "done":
		return cliDone(args[1:])
	case "views":
		return cliViews(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "tl: unknown command %q\n\n", args[0])
		printUsage(os.Stderr)
		return 1
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `tl - a terminal task manager

Usage:
  tl                   launch the TUI (default with no arguments)
  tl add <text...>     parse text and add a task to the current project
  tl ls [flags]        list tasks
  tl done <id>         mark a task complete
  tl views             list saved views (built-in and user-defined)
  tl views add <name> [flags]   create a user view (see 'tl views add' below)
  tl views rm <name>   delete a user view
  tl help              show this help
  tl version           show the version

ls flags:
  --all             list tasks across every project, not just the cwd's
  --inbox           list only tasks with no project (the inbox)
  --status <s>      filter by status: todo, in-progress, blocked
  --category <c>    filter by category name
  --due <range>     filter by due date: today, tomorrow, week, overdue
  --view <name>     run a saved view by name (case-insensitive; see tl views)
  --agenda          print tasks grouped into agenda buckets instead of a flat list
  --json            print a JSON array to stdout and nothing else

Capture syntax (shared by tl add and the TUI's 'a' key):
  #category                  !high !med !low (!h !m !l, !1 !2 !3)
  @todo @in-progress @blocked
  today, tonight, tomorrow (tmr, tmrw), yesterday
  monday..sunday (mon..sun), next <weekday>, next week, next month
  in N day(s)/week(s)/month(s), eod, eow
  2026-08-20, 20/08 or 20/08/2026 (day first), aug 20, 20 aug
  5pm, 5:30pm, 17:00, noon, midnight, at 5pm
  every day/daily, every N days, every week/weekly, every N weeks
  every <weekday>, every other <weekday>
  every month/monthly, every N months, every year/yearly, last day of month
`)
}

// cliAdd implements "tl add <text...>": parse the joined args as capture
// syntax and insert the resulting task into the cwd's project (or the
// inbox, if run outside a project).
func cliAdd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "tl add: missing task text")
		return 1
	}
	input := strings.Join(args, " ")

	parsed := ParseTask(input, time.Now())
	if parsed.Name == "" {
		fmt.Fprintln(os.Stderr, "tl add: no task name recognised in input")
		return 1
	}

	store := &Store{}
	if err := store.InitDb(); err != nil {
		fmt.Fprintf(os.Stderr, "tl: %v\n", err)
		return 2
	}
	defer store.Close()

	// Same rule as the TUI's 'a' key: a new task always targets the
	// project the command was run from, never some other scope - so
	// always say where it landed.
	cwd, _ := os.Getwd()
	root, _ := resolveProjectRoot(cwd)
	var proj *Project
	if root != nil {
		var err error
		proj, err = store.getOrCreateProject(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tl add: %v\n", err)
			return 2
		}
	}

	task := Task{
		Name:      parsed.Name,
		Status:    parsed.Status,
		Priority:  parsed.Priority,
		RecurRule: parsed.Recur,
	}
	if proj != nil {
		task.ProjectID = sql.NullInt64{Int64: proj.ID, Valid: true}
	}

	if parsed.Category != "" {
		cat, err := store.getOrCreateCategory(parsed.Category)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tl add: %v\n", err)
			return 2
		}
		if cat != nil {
			task.CategoryID = sql.NullInt64{Int64: cat.ID, Valid: true}
			task.CategoryName = cat.Name
		}
	}

	if parsed.DueAt != nil {
		task.DueAt = sql.NullTime{Time: *parsed.DueAt, Valid: true}
		task.DueHasTime = parsed.DueHasTime
	}

	if err := store.insertTask(&task); err != nil {
		fmt.Fprintf(os.Stderr, "tl add: %v\n", err)
		return 2
	}

	projName := "inbox"
	if proj != nil {
		projName = proj.Name
	}
	parts := []string{projName, task.Name}
	if task.DueAt.Valid {
		text, _ := formatDueBadge(task.DueAt.Time, task.DueHasTime, time.Now())
		parts = append(parts, "due "+text)
	}
	if label := priorityLabel(task.Priority); label != "" {
		parts = append(parts, label)
	}
	fmt.Printf("✓ %s\n", strings.Join(parts, " · "))
	return 0
}

// cliLs implements "tl ls [flags]". Every flag is just a field on
// TaskQuery, built here and handed to findTasks - no new SQL.
func cliLs(args []string) int {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	all := fs.Bool("all", false, "list tasks across every project")
	inbox := fs.Bool("inbox", false, "list only inbox (no-project) tasks")
	status := fs.String("status", "", "filter by status")
	category := fs.String("category", "", "filter by category name")
	due := fs.String("due", "", "filter by due date: today, tomorrow, week, overdue")
	view := fs.String("view", "", "run a saved view by name")
	agenda := fs.Bool("agenda", false, "print tasks grouped into agenda buckets")
	jsonOut := fs.Bool("json", false, "print a JSON array instead of a table")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "tl ls: %v\n", err)
		return 1
	}

	store := &Store{}
	if err := store.InitDb(); err != nil {
		fmt.Fprintf(os.Stderr, "tl: %v\n", err)
		return 2
	}
	defer store.Close()

	q := TaskQuery{IncludeDone: true}

	switch {
	case *inbox:
		q.NoProject = true
	case *all:
		// no project filter
	default:
		cwd, _ := os.Getwd()
		root, _ := resolveProjectRoot(cwd)
		if root != nil {
			proj, err := store.getOrCreateProject(root)
			if err != nil {
				fmt.Fprintf(os.Stderr, "tl ls: %v\n", err)
				return 2
			}
			id := proj.ID
			q.ProjectID = &id
		} else {
			q.NoProject = true
		}
	}

	if *status != "" {
		q.Statuses = []string{*status}
	}

	if *category != "" {
		cat, err := store.findCategoryByName(*category)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tl ls: %v\n", err)
			return 2
		}
		if cat == nil {
			fmt.Fprintf(os.Stderr, "tl ls: no such category %q\n", *category)
			return 1
		}
		q.CategoryID = &cat.ID
	}

	now := time.Now()
	if *due != "" {
		labels, err := dueRangeLabels(*due)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tl ls: %v\n", err)
			return 1
		}
		for _, label := range agendaBucketLabels {
			if labels[label] {
				q.DueBuckets = append(q.DueBuckets, label)
			}
		}

		// Coarse SQL bounds only - the exact bucket boundary (a date-only
		// task is overdue only after the *end* of its day; a task due
		// today may already be overdue if it carries a time that has
		// passed) is applied precisely afterwards by findTasks via
		// q.DueBuckets, so --due agrees with the agenda instead of the two
		// drifting apart (Task 0b). Bounds here just need to be wide enough
		// to contain every task any of the resulting DueBuckets could match.
		today := dateOnly(now)
		tomorrow := today.AddDate(0, 0, 1)
		switch *due {
		case "today":
			q.DueAfter, q.DueBefore = &today, &tomorrow
		case "tomorrow":
			dayAfter := tomorrow.AddDate(0, 0, 1)
			q.DueAfter, q.DueBefore = &tomorrow, &dayAfter
		case "week":
			daysToSunday := (7 - int(today.Weekday())) % 7
			end := today.AddDate(0, 0, daysToSunday+1)
			q.DueAfter, q.DueBefore = &today, &end
		case "overdue":
			// Overdue can include a task due earlier today (if it has a
			// time), so the coarse upper bound must extend through today.
			q.DueBefore = &tomorrow
		}
	}

	if *view != "" {
		views, err := store.getSavedViews()
		if err != nil {
			fmt.Fprintf(os.Stderr, "tl ls: %v\n", err)
			return 2
		}

		var matched *SavedView
		names := make([]string, len(views))
		for i := range views {
			names[i] = views[i].Name
			if strings.EqualFold(views[i].Name, *view) {
				matched = &views[i]
			}
		}
		if matched == nil {
			fmt.Fprintf(os.Stderr, "tl ls: no such view %q (valid views: %s)\n", *view, strings.Join(names, ", "))
			return 1
		}

		// Resolve the launch project the same way the plain scope switch
		// above does, for a view whose Scope is "project".
		cwd, _ := os.Getwd()
		root, _ := resolveProjectRoot(cwd)
		var launchProj *Project
		if root != nil {
			launchProj, err = store.getOrCreateProject(root)
			if err != nil {
				fmt.Fprintf(os.Stderr, "tl ls: %v\n", err)
				return 2
			}
		}

		lookupCategory := func(name string) *Category {
			cat, _ := store.findCategoryByName(name)
			return cat
		}

		vq, err := resolveViewSpec(matched.Spec, now, launchProj, lookupCategory)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tl ls: %v\n", err)
			return 2
		}

		// A view with an explicit scope replaces the scope resolved above;
		// Scope "" inherits it. Statuses/Due/IncludeDone are always the
		// view's, same merge rule as the TUI's currentQuery.
		if matched.Spec.Scope != "" {
			q.ProjectID, q.NoProject = vq.ProjectID, vq.NoProject
		}
		if vq.CategoryID != nil {
			q.CategoryID = vq.CategoryID
		}
		q.Statuses = vq.Statuses
		q.DueAfter, q.DueBefore, q.DueSet = vq.DueAfter, vq.DueBefore, vq.DueSet
		q.DueBuckets = vq.DueBuckets // the view's own due bounds are authoritative, not --due's
		q.IncludeDone = vq.IncludeDone
	}

	tasks, err := store.findTasks(q)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tl ls: %v\n", err)
		return 2
	}

	if *agenda {
		printAgendaText(bucketAgenda(tasks, now))
		return 0
	}

	if *jsonOut {
		printTasksJSON(tasks)
		return 0
	}

	printTasksTable(tasks)
	return 0
}

// cliDone implements "tl done <id>".
func cliDone(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "tl done: missing task id")
		return 1
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tl done: invalid task id %q\n", args[0])
		return 1
	}

	store := &Store{}
	if err := store.InitDb(); err != nil {
		fmt.Fprintf(os.Stderr, "tl: %v\n", err)
		return 2
	}
	defer store.Close()

	task, err := findTaskByID(store, id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tl done: %v\n", err)
		return 2
	}
	if task == nil {
		fmt.Fprintf(os.Stderr, "tl done: no task with id %d\n", id)
		return 1
	}

	newID, err := store.completeTask(*task, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "tl done: %v\n", err)
		return 2
	}

	if newID != 0 {
		newTask, err := findTaskByID(store, newID)
		if err == nil && newTask != nil && newTask.DueAt.Valid {
			fmt.Printf("✓ completed · %s · next occurrence %s\n", task.Name, newTask.DueAt.Time.Format("Mon 2 Jan"))
			return 0
		}
	}

	fmt.Printf("✓ marked #%d %q complete\n", id, task.Name)
	return 0
}

// cliViews implements "tl views": lists saved views (the four built-ins,
// always positions 1-4, then any user-defined views) as
// "position  name  (spec summary)", or dispatches to "add"/"rm" when given a
// recognised subcommand. An unrecognised subcommand is a usage error (exit
// 1), not a silently-ignored one - "tl views add \"My View\"" with no
// filter flags used to fall through here, print the list and exit 0, which
// is the worst failure mode available: it looks like it worked.
func cliViews(args []string) int {
	if len(args) == 0 {
		return cliViewsList()
	}

	switch args[0] {
	case "add":
		return cliViewsAdd(args[1:])
	case "rm":
		return cliViewsRm(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "tl views: unknown subcommand %q\n\n", args[0])
		printViewsUsage(os.Stderr)
		return 1
	}
}

func printViewsUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  tl views                      list saved views (built-in and user-defined)
  tl views add <name> [flags]   create a user view
  tl views rm <name>            delete a user view

add flags (at least one is required):
  --due <range>     today, tomorrow, week, or overdue
  --status <s>      repeatable
  --category <c>
  --scope <s>       project, all, or inbox
  --include-done
`)
}

func cliViewsList() int {
	store := &Store{}
	if err := store.InitDb(); err != nil {
		fmt.Fprintf(os.Stderr, "tl: %v\n", err)
		return 2
	}
	defer store.Close()

	views, err := store.getSavedViews()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tl views: %v\n", err)
		return 2
	}

	for _, v := range views {
		fmt.Printf("%d  %s  (%s)\n", v.Position, v.Name, summarizeViewSpec(v.Spec))
	}
	return 0
}

// stringSliceFlag accumulates repeated occurrences of the same flag (e.g.
// "--status todo --status blocked") - the standard flag package only keeps
// the last value for a string flag, so ViewSpec.Statuses (which can hold
// several) needs its own flag.Value.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// cliViewsAdd implements "tl views add <name> [flags]". The name is
// positional and must come first (flag.FlagSet stops parsing flags at the
// first non-flag argument, so it is popped off before fs.Parse rather than
// looked for among fs.Args() afterwards).
func cliViewsAdd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "tl views add: missing view name")
		return 1
	}
	name := args[0]

	fs := flag.NewFlagSet("views add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	due := fs.String("due", "", "filter by due date: today, tomorrow, week, overdue")
	var statuses stringSliceFlag
	fs.Var(&statuses, "status", "filter by status (repeatable)")
	category := fs.String("category", "", "filter by category name")
	scope := fs.String("scope", "", "scope: project, all, inbox")
	includeDone := fs.Bool("include-done", false, "include completed tasks")

	if err := fs.Parse(args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "tl views add: %v\n", err)
		return 1
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "tl views add: unexpected argument %q\n", fs.Arg(0))
		return 1
	}

	// Checked before "at least one flag": a bare "tl views add Today" is
	// both a name collision and an empty spec, and the collision is the
	// more informative reason to report. store.saveView enforces this too
	// (see errBuiltInViewName) - this is just the fast, friendly rejection
	// before ever opening the store.
	if isBuiltInViewName(name) {
		fmt.Fprintf(os.Stderr, "tl views add: %q collides with a built-in view\n", name)
		return 1
	}

	if *due == "" && len(statuses) == 0 && *category == "" && *scope == "" && !*includeDone {
		fmt.Fprintln(os.Stderr, "tl views add: at least one of --due, --status, --category, --scope, --include-done is required (an empty view means nothing)")
		return 1
	}

	if *due != "" {
		if _, err := dueRangeLabels(*due); err != nil {
			fmt.Fprintf(os.Stderr, "tl views add: %v\n", err)
			return 1
		}
	}
	if *scope != "" && *scope != "project" && *scope != "all" && *scope != "inbox" {
		fmt.Fprintf(os.Stderr, "tl views add: invalid --scope %q (want project, all, or inbox)\n", *scope)
		return 1
	}

	spec := ViewSpec{
		Scope:       *scope,
		Due:         *due,
		Statuses:    []string(statuses),
		Category:    *category,
		IncludeDone: *includeDone,
	}

	store := &Store{}
	if err := store.InitDb(); err != nil {
		fmt.Fprintf(os.Stderr, "tl: %v\n", err)
		return 2
	}
	defer store.Close()

	if err := store.saveView(name, spec); err != nil {
		switch {
		case strings.Contains(err.Error(), "UNIQUE constraint failed"):
			fmt.Fprintf(os.Stderr, "tl views add: a view named %q already exists\n", name)
			return 1
		case errors.Is(err, errBuiltInViewName):
			fmt.Fprintf(os.Stderr, "tl views add: %q collides with a built-in view\n", name)
			return 1
		default:
			fmt.Fprintf(os.Stderr, "tl views add: %v\n", err)
			return 2
		}
	}

	fmt.Printf("✓ saved view %q\n", name)
	return 0
}

// cliViewsRm implements "tl views rm <name>".
func cliViewsRm(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "tl views rm: missing view name")
		return 1
	}
	name := args[0]

	if v := findBuiltInView(name); v != nil {
		fmt.Fprintf(os.Stderr, "tl views rm: cannot delete built-in view %q\n", v.Name)
		return 1
	}

	store := &Store{}
	if err := store.InitDb(); err != nil {
		fmt.Fprintf(os.Stderr, "tl: %v\n", err)
		return 2
	}
	defer store.Close()

	views, err := store.getSavedViews()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tl views rm: %v\n", err)
		return 2
	}

	var matched *SavedView
	var userNames []string
	for i := range views {
		if views[i].BuiltIn {
			continue
		}
		userNames = append(userNames, views[i].Name)
		if strings.EqualFold(views[i].Name, name) {
			matched = &views[i]
		}
	}
	if matched == nil {
		fmt.Fprintf(os.Stderr, "tl views rm: no such view %q (valid views: %s)\n", name, strings.Join(userNames, ", "))
		return 1
	}

	if err := store.deleteView(matched.ID); err != nil {
		fmt.Fprintf(os.Stderr, "tl views rm: %v\n", err)
		return 2
	}

	fmt.Printf("✓ deleted view %q\n", matched.Name)
	return 0
}

// summarizeViewSpec renders a short, human-readable summary of a ViewSpec's
// non-default fields for "tl views" output.
func summarizeViewSpec(s ViewSpec) string {
	var parts []string
	if s.Scope != "" {
		parts = append(parts, "scope="+s.Scope)
	}
	if s.Due != "" {
		parts = append(parts, "due="+s.Due)
	}
	if len(s.Statuses) > 0 {
		parts = append(parts, "status="+strings.Join(s.Statuses, ","))
	}
	if s.Category != "" {
		parts = append(parts, "category="+s.Category)
	}
	if s.IncludeDone {
		parts = append(parts, "include-done")
	}
	if len(parts) == 0 {
		return "no filter"
	}
	return strings.Join(parts, " ")
}

// findTaskByID looks a task up by id across every project and status.
// TaskQuery has no ID field (deliberately - it is a filter language, not a
// lookup key), so this scans findTasks(IncludeDone) in Go instead of adding
// new SQL for a single CLI command.
func findTaskByID(s *Store, id int64) (*Task, error) {
	tasks, err := s.findTasks(TaskQuery{IncludeDone: true})
	if err != nil {
		return nil, err
	}
	for i := range tasks {
		if tasks[i].ID == id {
			return &tasks[i], nil
		}
	}
	return nil, nil
}

func priorityLabel(p int) string {
	switch p {
	case PriorityLow:
		return "!low"
	case PriorityMed:
		return "!med"
	case PriorityHigh:
		return "!high"
	}
	return ""
}

// formatTaskLine renders one plain-text task line, shared by the flat
// "tl ls" table and the bucketed "tl ls --agenda" printer.
func formatTaskLine(t Task) string {
	checkbox := "[ ]"
	if t.Completed == 1 {
		checkbox = "[x]"
	}

	line := fmt.Sprintf("%s #%d %s", checkbox, t.ID, t.Name)

	if t.CategoryName != "" {
		line += " [" + t.CategoryName + "]"
	}

	if t.DueAt.Valid {
		text, overdue := formatDueBadge(t.DueAt.Time, t.DueHasTime, time.Now())
		style := dueBadgeStyle
		if overdue {
			style = overdueBadgeStyle
		}
		line += " " + style.Render(text)
	}

	if label := priorityLabel(t.Priority); label != "" {
		line += " " + label
	}

	line += " @" + t.Status

	if t.RecurRule != "" {
		line += " (" + t.RecurRule + ")"
	}

	projName := t.ProjectName
	if projName == "" {
		projName = "inbox"
	}
	line += " (" + projName + ")"

	return line
}

func printTasksTable(tasks []Task) {
	if len(tasks) == 0 {
		fmt.Println("No tasks.")
		return
	}

	for _, t := range tasks {
		fmt.Println(formatTaskLine(t))
	}
}

// printAgendaText prints the same buckets as the TUI's agenda view, plain
// text: a "Label (count)" heading per non-empty bucket, then each task's
// line indented under it.
func printAgendaText(buckets []agendaBucket) {
	if len(buckets) == 0 {
		fmt.Println("No tasks.")
		return
	}

	for _, b := range buckets {
		fmt.Printf("%s (%d)\n", b.Label, len(b.Tasks))
		for _, t := range b.Tasks {
			fmt.Println("  " + formatTaskLine(t))
		}
	}
}

// taskJSON is the wire shape for "tl ls --json" - a plain, self-describing
// shape rather than marshalling Task directly, since Task's sql.Null*
// fields don't serialise into anything a downstream consumer would want.
type taskJSON struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Completed  bool   `json:"completed"`
	Status     string `json:"status"`
	Category   string `json:"category,omitempty"`
	Project    string `json:"project,omitempty"`
	Priority   int    `json:"priority"`
	DueAt      string `json:"due_at,omitempty"`
	DueHasTime bool   `json:"due_has_time,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

func toTaskJSON(t Task) taskJSON {
	tj := taskJSON{
		ID:        t.ID,
		Name:      t.Name,
		Completed: t.Completed == 1,
		Status:    t.Status,
		Category:  t.CategoryName,
		Project:   t.ProjectName,
		Priority:  t.Priority,
		CreatedAt: t.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt: t.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
	if t.DueAt.Valid {
		tj.DueAt = t.DueAt.Time.Format("2006-01-02 15:04:05")
		tj.DueHasTime = t.DueHasTime
	}
	return tj
}

// printTasksJSON prints a JSON array to stdout and nothing else, so it can
// be piped straight into jq or another tool.
func printTasksJSON(tasks []Task) {
	out := make([]taskJSON, len(tasks))
	for i, t := range tasks {
		out[i] = toTaskJSON(t)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}
