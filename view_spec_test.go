package main

import (
	"database/sql"
	"testing"
	"time"
)

func TestResolveViewSpec_BuiltIns(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local) // Wednesday

	tests := []struct {
		name string
		spec ViewSpec
		want TaskQuery
	}{
		{
			name: "Today",
			spec: ViewSpec{Due: "today"},
			want: TaskQuery{DueAfter: tp(dt(2026, 8, 12, 0, 0)), DueBefore: tp(dt(2026, 8, 13, 0, 0)), DueBuckets: []string{"Today"}},
		},
		{
			name: "Overdue",
			spec: ViewSpec{Due: "overdue"},
			// DueBefore extends through tomorrow, not just today's midnight
			// - a task due earlier today (with a time already passed) is
			// Overdue too, and a tight bound here would exclude it from the
			// SQL fetch before filterByDueBucket ever got a chance to keep
			// it (Task 1's bug).
			want: TaskQuery{DueBefore: tp(dt(2026, 8, 13, 0, 0)), DueBuckets: []string{"Overdue"}},
		},
		{
			name: "Next 7 days (week)",
			spec: ViewSpec{Due: "week"},
			want: TaskQuery{DueAfter: tp(dt(2026, 8, 12, 0, 0)), DueBefore: tp(dt(2026, 8, 17, 0, 0)), DueBuckets: []string{"Today", "Tomorrow", "This week"}},
		},
		{
			name: "Blocked",
			spec: ViewSpec{Statuses: []string{"blocked"}},
			want: TaskQuery{Statuses: []string{"blocked"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveViewSpec(tt.spec, now, nil, nil)
			if err != nil {
				t.Fatalf("resolveViewSpec: %v", err)
			}
			assertQueryEqual(t, got, tt.want)
		})
	}
}

// TestResolveViewSpec_TaskDueEarlierToday is the exact correctness bug from
// Task 1: a task due 08:00 today, viewed after that time, must be absent
// from the "Today" view and present in "Overdue" - dueBucketOf (via
// DueBuckets/filterByDueBucket in findTasks) is what makes the Today built-
// in agree with the agenda instead of including anything the coarse
// DueAfter/DueBefore bounds alone would let through.
func TestResolveViewSpec_TaskDueEarlierToday(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local)
	task := Task{DueAt: sql.NullTime{Time: time.Date(2026, 8, 12, 8, 0, 0, 0, time.Local), Valid: true}, DueHasTime: true}

	todayQ, err := resolveViewSpec(ViewSpec{Due: "today"}, now, nil, nil)
	if err != nil {
		t.Fatalf("resolveViewSpec(today): %v", err)
	}
	if got := filterByDueBucket([]Task{task}, now, bucketSet(todayQ.DueBuckets)); len(got) != 0 {
		t.Errorf("expected the Today view to exclude a task due earlier today, got %+v", got)
	}

	overdueQ, err := resolveViewSpec(ViewSpec{Due: "overdue"}, now, nil, nil)
	if err != nil {
		t.Fatalf("resolveViewSpec(overdue): %v", err)
	}
	// The coarse bound must not itself exclude the row before the bucket
	// filter runs.
	if overdueQ.DueBefore == nil || !task.DueAt.Time.Before(*overdueQ.DueBefore) {
		t.Fatalf("overdue's coarse DueBefore bound (%v) must be wide enough to include a task due earlier today (%v)", overdueQ.DueBefore, task.DueAt.Time)
	}
	if got := filterByDueBucket([]Task{task}, now, bucketSet(overdueQ.DueBuckets)); len(got) != 1 {
		t.Errorf("expected the Overdue view to include a task due earlier today, got %+v", got)
	}
}

// bucketSet converts a DueBuckets slice to the map[string]bool
// filterByDueBucket expects.
func bucketSet(labels []string) map[string]bool {
	set := make(map[string]bool, len(labels))
	for _, l := range labels {
		set[l] = true
	}
	return set
}

func TestResolveViewSpec_Scope(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local)
	launch := &Project{ID: 42}

	t.Run("project scope uses launch project", func(t *testing.T) {
		got, err := resolveViewSpec(ViewSpec{Scope: "project"}, now, launch, nil)
		if err != nil {
			t.Fatalf("resolveViewSpec: %v", err)
		}
		if got.ProjectID == nil || *got.ProjectID != 42 {
			t.Errorf("ProjectID = %v, want 42", got.ProjectID)
		}
	})

	t.Run("project scope with no launch project falls back to inbox", func(t *testing.T) {
		got, err := resolveViewSpec(ViewSpec{Scope: "project"}, now, nil, nil)
		if err != nil {
			t.Fatalf("resolveViewSpec: %v", err)
		}
		if !got.NoProject {
			t.Errorf("expected NoProject true when scope=project but no launch project, got %+v", got)
		}
	})

	t.Run("all scope sets no project filter", func(t *testing.T) {
		got, err := resolveViewSpec(ViewSpec{Scope: "all"}, now, launch, nil)
		if err != nil {
			t.Fatalf("resolveViewSpec: %v", err)
		}
		if got.ProjectID != nil || got.NoProject {
			t.Errorf("expected no project filter for scope=all, got %+v", got)
		}
	})

	t.Run("inbox scope", func(t *testing.T) {
		got, err := resolveViewSpec(ViewSpec{Scope: "inbox"}, now, launch, nil)
		if err != nil {
			t.Fatalf("resolveViewSpec: %v", err)
		}
		if !got.NoProject {
			t.Errorf("expected NoProject true for scope=inbox, got %+v", got)
		}
	})

	t.Run("empty scope inherits: leaves both unset", func(t *testing.T) {
		got, err := resolveViewSpec(ViewSpec{}, now, launch, nil)
		if err != nil {
			t.Fatalf("resolveViewSpec: %v", err)
		}
		if got.ProjectID != nil || got.NoProject {
			t.Errorf("expected scope fields left unset for inherit, got %+v", got)
		}
	})

	t.Run("unknown scope is an error", func(t *testing.T) {
		if _, err := resolveViewSpec(ViewSpec{Scope: "bogus"}, now, launch, nil); err == nil {
			t.Errorf("expected an error for an unknown scope, got nil")
		}
	})
}

func TestResolveViewSpec_Category(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local)
	lookup := func(name string) *Category {
		if name == "work" {
			return &Category{ID: 7, Name: "work"}
		}
		return nil
	}

	t.Run("resolves a known category", func(t *testing.T) {
		got, err := resolveViewSpec(ViewSpec{Category: "work"}, now, nil, lookup)
		if err != nil {
			t.Fatalf("resolveViewSpec: %v", err)
		}
		if got.CategoryID == nil || *got.CategoryID != 7 {
			t.Errorf("CategoryID = %v, want 7", got.CategoryID)
		}
	})

	t.Run("unknown category is an error, not a silent empty filter", func(t *testing.T) {
		if _, err := resolveViewSpec(ViewSpec{Category: "ghost"}, now, nil, lookup); err == nil {
			t.Errorf("expected an error for an unknown category, got nil")
		}
	})

	t.Run("category given but no lookup provided is an error", func(t *testing.T) {
		if _, err := resolveViewSpec(ViewSpec{Category: "work"}, now, nil, nil); err == nil {
			t.Errorf("expected an error when lookupCategory is nil, got nil")
		}
	})
}

// TestResolveViewSpec_UnknownDueIsError is the specific case called out in
// the plan: an unrecognised Due value must be an error, not a silent empty
// filter that would quietly return every task.
func TestResolveViewSpec_UnknownDueIsError(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local)
	_, err := resolveViewSpec(ViewSpec{Due: "next-thursday-ish"}, now, nil, nil)
	if err == nil {
		t.Fatalf("expected an error for an unknown Due value, got nil")
	}
}

func TestResolveViewSpec_IncludeDone(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local)
	got, err := resolveViewSpec(ViewSpec{IncludeDone: true}, now, nil, nil)
	if err != nil {
		t.Fatalf("resolveViewSpec: %v", err)
	}
	if !got.IncludeDone {
		t.Errorf("expected IncludeDone true to propagate")
	}
}

// TestViewSpec_JSONRoundTrip proves marshalViewSpec/unmarshalViewSpec
// reproduce an equivalent ViewSpec - the same discipline as
// ParseTask/FormatTask and ParseRecur/RecurRule.String.
func TestViewSpec_JSONRoundTrip(t *testing.T) {
	specs := []ViewSpec{
		{},
		{Scope: "project", Due: "today"},
		{Scope: "all", Due: "overdue", Statuses: []string{"blocked", "todo"}, Category: "work", IncludeDone: true},
		{Due: "week"},
	}

	for _, spec := range specs {
		s, err := marshalViewSpec(spec)
		if err != nil {
			t.Fatalf("marshalViewSpec(%+v): %v", spec, err)
		}
		got, err := unmarshalViewSpec(s)
		if err != nil {
			t.Fatalf("unmarshalViewSpec(%q): %v", s, err)
		}

		if got.Scope != spec.Scope || got.Due != spec.Due || got.Category != spec.Category || got.IncludeDone != spec.IncludeDone {
			t.Errorf("round trip mismatch: %+v -> %q -> %+v", spec, s, got)
		}
		if len(got.Statuses) != len(spec.Statuses) {
			t.Errorf("Statuses round trip mismatch: %+v -> %q -> %+v", spec, s, got)
		}
		for i := range spec.Statuses {
			if i >= len(got.Statuses) || got.Statuses[i] != spec.Statuses[i] {
				t.Errorf("Statuses round trip mismatch: %+v -> %q -> %+v", spec, s, got)
			}
		}
	}
}

func TestBuiltInViews_PositionsAndNames(t *testing.T) {
	want := []string{"Today", "Overdue", "Next 7 days", "Blocked"}
	if len(builtInViews) != len(want) {
		t.Fatalf("expected %d built-in views, got %d", len(want), len(builtInViews))
	}
	for i, v := range builtInViews {
		if v.Name != want[i] {
			t.Errorf("builtInViews[%d].Name = %q, want %q", i, v.Name, want[i])
		}
		if v.Position != i+1 {
			t.Errorf("builtInViews[%d].Position = %d, want %d", i, v.Position, i+1)
		}
		if !v.BuiltIn {
			t.Errorf("builtInViews[%d].BuiltIn = false, want true", i)
		}
	}
}

// tp returns a *time.Time, for building TaskQuery literals in table tests.
func tp(t time.Time) *time.Time { return &t }

// assertQueryEqual compares the fields resolveViewSpec actually sets,
// dereferencing pointer fields for a value comparison.
func assertQueryEqual(t *testing.T, got, want TaskQuery) {
	t.Helper()
	if !timePtrEqual(got.DueAfter, want.DueAfter) {
		t.Errorf("DueAfter = %v, want %v", deref(got.DueAfter), deref(want.DueAfter))
	}
	if !timePtrEqual(got.DueBefore, want.DueBefore) {
		t.Errorf("DueBefore = %v, want %v", deref(got.DueBefore), deref(want.DueBefore))
	}
	if len(got.Statuses) != len(want.Statuses) {
		t.Errorf("Statuses = %v, want %v", got.Statuses, want.Statuses)
	} else {
		for i := range want.Statuses {
			if got.Statuses[i] != want.Statuses[i] {
				t.Errorf("Statuses = %v, want %v", got.Statuses, want.Statuses)
			}
		}
	}
	if len(got.DueBuckets) != len(want.DueBuckets) {
		t.Errorf("DueBuckets = %v, want %v", got.DueBuckets, want.DueBuckets)
	} else {
		for i := range want.DueBuckets {
			if got.DueBuckets[i] != want.DueBuckets[i] {
				t.Errorf("DueBuckets = %v, want %v", got.DueBuckets, want.DueBuckets)
			}
		}
	}
}

func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func deref(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
