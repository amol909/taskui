package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ViewSpec is a saved, named filter. Stored as JSON in the views table (see
// store.go's getSavedViews/saveView/deleteView). It deliberately mirrors the
// same filter language TaskQuery already speaks - resolveViewSpec is the
// only place that translates one into the other - rather than inventing a
// second one.
type ViewSpec struct {
	Scope       string   `json:"scope"` // "project" | "all" | "inbox" | "" = inherit
	Due         string   `json:"due"`   // "today" | "tomorrow" | "week" | "overdue" | ""
	Statuses    []string `json:"statuses,omitempty"`
	Category    string   `json:"category,omitempty"`
	IncludeDone bool     `json:"include_done,omitempty"`
}

// SavedView is one named, positioned entry in the view bar / palette. Built-
// ins (see builtInViews) always occupy positions 1-4 and BuiltIn == true;
// user views (persisted in the views table) come after.
type SavedView struct {
	ID       int64
	Name     string
	Spec     ViewSpec
	Position int
	BuiltIn  bool
}

// builtInViews are defined in code, not the database, so they cannot be
// renamed or deleted out from under the "1"-"9" keybindings or the palette's
// "View: ..." commands - see the AGENTS.md note on this decision. Position
// is reassigned by getSavedViews (built-ins first, then DB rows), but is set
// here too so builtInViews alone is already meaningful to callers that don't
// go through the store (e.g. tests, the palette before a store is wired up).
var builtInViews = []SavedView{
	{ID: -1, Name: "Today", Spec: ViewSpec{Due: "today"}, Position: 1, BuiltIn: true},
	{ID: -2, Name: "Overdue", Spec: ViewSpec{Due: "overdue"}, Position: 2, BuiltIn: true},
	{ID: -3, Name: "Next 7 days", Spec: ViewSpec{Due: "week"}, Position: 3, BuiltIn: true},
	{ID: -4, Name: "Blocked", Spec: ViewSpec{Statuses: []string{"blocked"}}, Position: 4, BuiltIn: true},
}

// isBuiltInViewName reports whether name collides case-insensitively with
// one of the four built-in views (Today, Overdue, Next 7 days, Blocked).
// Shared by store.saveView (the actual enforcement point) and the CLI/TUI
// (which check it first for a fast, friendly rejection before ever touching
// the store) rather than each re-walking builtInViews itself.
func isBuiltInViewName(name string) bool {
	return findBuiltInView(name) != nil
}

// findBuiltInView returns the built-in view whose Name matches name case-
// insensitively, or nil - used where the canonical name/casing is needed
// (e.g. "cannot delete built-in view \"Today\"" regardless of how the user
// typed it), not just whether one matches.
func findBuiltInView(name string) *SavedView {
	for i := range builtInViews {
		if strings.EqualFold(builtInViews[i].Name, name) {
			return &builtInViews[i]
		}
	}
	return nil
}

// resolveViewSpec turns a spec into a TaskQuery. Pure: now and the launch
// project are passed in, never read from the environment. lookupCategory
// resolves a category name to a *Category (nil = not found); it may be nil
// when s.Category is always "" for the caller's use case.
//
// Scope == "" means "inherit" - resolveViewSpec leaves the query's
// ProjectID/NoProject fields unset in that case, and callers merging the
// result into an already-scoped TaskQuery (e.g. the TUI's refreshTasks) must
// leave their own scope fields alone rather than overwrite them with this
// zero value.
func resolveViewSpec(s ViewSpec, now time.Time, launch *Project, lookupCategory func(string) *Category) (TaskQuery, error) {
	q := TaskQuery{IncludeDone: s.IncludeDone}

	switch s.Scope {
	case "":
		// inherit - leave ProjectID/NoProject unset
	case "project":
		if launch != nil {
			id := launch.ID
			q.ProjectID = &id
		} else {
			q.NoProject = true
		}
	case "all":
		// no project filter
	case "inbox":
		q.NoProject = true
	default:
		return TaskQuery{}, fmt.Errorf("resolveViewSpec: unknown scope %q", s.Scope)
	}

	if len(s.Statuses) > 0 {
		q.Statuses = append([]string(nil), s.Statuses...)
	}

	if s.Category != "" {
		if lookupCategory == nil {
			return TaskQuery{}, fmt.Errorf("resolveViewSpec: category %q given but no category lookup provided", s.Category)
		}
		cat := lookupCategory(s.Category)
		if cat == nil {
			return TaskQuery{}, fmt.Errorf("resolveViewSpec: no such category %q", s.Category)
		}
		id := cat.ID
		q.CategoryID = &id
	}

	if s.Due != "" {
		// DueBuckets is the precise filter (applied by findTasks via
		// dueBucketOf, after the SQL query runs); dueRangeLabels maps a Due
		// value to the exact agenda bucket labels it means, so this and
		// bucketAgenda/cli.go's --due agree by construction rather than by
		// each reimplementing the mapping. The DueAfter/DueBefore bounds set
		// below are only a coarse SQL pre-filter and must stay wide enough
		// to contain every task any of those buckets could match - see the
		// "overdue" case, which would otherwise cut off a task due earlier
		// today before the bucket filter ever saw it.
		labels, err := dueRangeLabels(s.Due)
		if err != nil {
			return TaskQuery{}, fmt.Errorf("resolveViewSpec: %w", err)
		}
		for _, label := range agendaBucketLabels {
			if labels[label] {
				q.DueBuckets = append(q.DueBuckets, label)
			}
		}

		today := dateOnly(now)
		switch s.Due {
		case "today":
			start := today
			end := start.AddDate(0, 0, 1)
			q.DueAfter, q.DueBefore = &start, &end
		case "tomorrow":
			start := today.AddDate(0, 0, 1)
			end := start.AddDate(0, 0, 1)
			q.DueAfter, q.DueBefore = &start, &end
		case "week":
			daysToSunday := (7 - int(today.Weekday())) % 7
			end := today.AddDate(0, 0, daysToSunday+1)
			q.DueAfter, q.DueBefore = &today, &end
		case "overdue":
			// Overdue can include a task due earlier today (if it carries a
			// time that has already passed), so the coarse upper bound must
			// extend through today - filterByDueBucket narrows it precisely
			// afterwards. Matches cli.go's own --due=overdue handling.
			tomorrow := today.AddDate(0, 0, 1)
			q.DueBefore = &tomorrow
		}
	}

	return q, nil
}

// marshalViewSpec/unmarshalViewSpec are thin JSON wrappers kept next to
// ViewSpec so store.go's saveView/getSavedViews don't need to know the
// encoding, only that spec round-trips through a string column.
func marshalViewSpec(s ViewSpec) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalViewSpec(s string) (ViewSpec, error) {
	var spec ViewSpec
	if err := json.Unmarshal([]byte(s), &spec); err != nil {
		return ViewSpec{}, err
	}
	return spec, nil
}
