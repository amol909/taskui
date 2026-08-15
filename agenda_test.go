package main

import (
	"database/sql"
	"reflect"
	"testing"
	"time"
)

// TestBucketAgenda covers the full bucket set against a fixed now (Wed 12
// Aug 2026, so "this week" runs through Sunday 16 Aug), including the
// date-only end-of-day overdue boundary: a date-only task due "today" is
// not overdue no matter how late in the day now is, but one due "yesterday"
// already is - even though both differ from now by less than 24 hours in
// the naive sense.
func TestBucketAgenda(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local) // Wednesday

	mk := func(name string, due time.Time, hasTime bool, priority int) Task {
		return Task{Name: name, DueAt: sql.NullTime{Time: due, Valid: true}, DueHasTime: hasTime, Priority: priority}
	}

	overdueDateOnly := mk("overdue date-only", dt(2026, 8, 10, 0, 0), false, PriorityNone)
	overdueBoundary := mk("overdue boundary (due yesterday)", dt(2026, 8, 11, 0, 0), false, PriorityNone)
	overdueTimed := mk("overdue timed (earlier today)", dt(2026, 8, 12, 9, 0), true, PriorityNone)
	todayDateOnly := mk("today date-only", dt(2026, 8, 12, 0, 0), false, PriorityLow)
	todayTimedFuture := mk("today timed (later today)", dt(2026, 8, 12, 15, 0), true, PriorityHigh)
	tomorrow := mk("tomorrow", dt(2026, 8, 13, 0, 0), false, PriorityNone)
	thisWeekFri := mk("this week friday", dt(2026, 8, 14, 0, 0), false, PriorityNone)
	thisWeekSundayBoundary := mk("this week sunday boundary", dt(2026, 8, 16, 0, 0), false, PriorityNone)
	later := mk("later", dt(2026, 8, 20, 0, 0), false, PriorityNone)
	someday := Task{Name: "someday"}

	tasks := []Task{
		overdueDateOnly, overdueBoundary, overdueTimed,
		todayDateOnly, todayTimedFuture,
		tomorrow,
		thisWeekFri, thisWeekSundayBoundary,
		later,
		someday,
	}

	buckets := bucketAgenda(tasks, now)

	got := make(map[string][]string, len(buckets))
	var labelOrder []string
	for _, b := range buckets {
		labelOrder = append(labelOrder, b.Label)
		var names []string
		for _, tsk := range b.Tasks {
			names = append(names, tsk.Name)
		}
		got[b.Label] = names
	}

	want := map[string][]string{
		"Overdue":   {"overdue date-only", "overdue boundary (due yesterday)", "overdue timed (earlier today)"},
		"Today":     {"today date-only", "today timed (later today)"},
		"Tomorrow":  {"tomorrow"},
		"This week": {"this week friday", "this week sunday boundary"},
		"Later":     {"later"},
		"Someday":   {"someday"},
	}
	wantOrder := []string{"Overdue", "Today", "Tomorrow", "This week", "Later", "Someday"}

	if !reflect.DeepEqual(labelOrder, wantOrder) {
		t.Errorf("bucket order = %v, want %v", labelOrder, wantOrder)
	}

	for label, wantNames := range want {
		if gotNames := got[label]; !reflect.DeepEqual(gotNames, wantNames) {
			t.Errorf("bucket %q = %v, want %v", label, gotNames, wantNames)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d non-empty buckets, want %d: %v", len(got), len(want), got)
	}
}

// TestBucketAgenda_SkipsEmptyBuckets proves a bucket with no tasks is
// omitted entirely rather than appearing with a zero count.
func TestBucketAgenda_SkipsEmptyBuckets(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local)
	buckets := bucketAgenda([]Task{{Name: "only someday"}}, now)
	if len(buckets) != 1 || buckets[0].Label != "Someday" {
		t.Fatalf("expected exactly one Someday bucket, got %+v", buckets)
	}
}

// TestBucketAgenda_SortByDueTimeThenPriorityDescending proves the within-
// bucket sort: due time ascending first, then priority descending for ties.
func TestBucketAgenda_SortByDueTimeThenPriorityDescending(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local)
	due := dt(2026, 8, 20, 0, 0) // same "Later" bucket, identical due time

	low := Task{Name: "low", DueAt: sql.NullTime{Time: due, Valid: true}, Priority: PriorityLow}
	high := Task{Name: "high", DueAt: sql.NullTime{Time: due, Valid: true}, Priority: PriorityHigh}
	med := Task{Name: "med", DueAt: sql.NullTime{Time: due, Valid: true}, Priority: PriorityMed}
	earlier := Task{Name: "earlier", DueAt: sql.NullTime{Time: dt(2026, 8, 18, 0, 0), Valid: true}, Priority: PriorityNone}

	buckets := bucketAgenda([]Task{low, high, med, earlier}, now)
	if len(buckets) != 1 || buckets[0].Label != "Later" {
		t.Fatalf("expected a single Later bucket, got %+v", buckets)
	}

	var names []string
	for _, tsk := range buckets[0].Tasks {
		names = append(names, tsk.Name)
	}
	want := []string{"earlier", "high", "med", "low"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("order = %v, want %v", names, want)
	}
}

// TestDueBucketOf_AgreesWithBucketAgenda_TaskDueEarlierToday is the Task 0b
// regression test: a task due 09:00 today, queried at 15:00, must land in
// the same bucket ("Overdue") whether it goes through dueBucketOf directly
// (what "tl ls --due" now uses) or through bucketAgenda (what the agenda
// view uses) - the two surfaces must not disagree about the same word.
func TestDueBucketOf_AgreesWithBucketAgenda_TaskDueEarlierToday(t *testing.T) {
	now := time.Date(2026, 8, 12, 15, 0, 0, 0, time.Local)
	task := Task{Name: "standup", DueAt: sql.NullTime{Time: dt(2026, 8, 12, 9, 0), Valid: true}, DueHasTime: true}

	gotDirect := dueBucketOf(task, now).Label
	if gotDirect != "Overdue" {
		t.Fatalf("dueBucketOf = %q, want %q", gotDirect, "Overdue")
	}

	buckets := bucketAgenda([]Task{task}, now)
	if len(buckets) != 1 || buckets[0].Label != "Overdue" {
		t.Fatalf("bucketAgenda = %+v, want a single Overdue bucket", buckets)
	}

	// Under the old --due semantics (bare midnight-boundary comparison) this
	// task would have been "today", not "overdue" - dueRangeLabels("today")
	// must no longer claim it.
	todayLabels, err := dueRangeLabels("today")
	if err != nil {
		t.Fatalf("dueRangeLabels(today): %v", err)
	}
	if filtered := filterByDueBucket([]Task{task}, now, todayLabels); len(filtered) != 0 {
		t.Errorf("expected a task overdue since 09:00 to be excluded from --due today, got %+v", filtered)
	}

	overdueLabels, err := dueRangeLabels("overdue")
	if err != nil {
		t.Fatalf("dueRangeLabels(overdue): %v", err)
	}
	if filtered := filterByDueBucket([]Task{task}, now, overdueLabels); len(filtered) != 1 {
		t.Errorf("expected the task to be included in --due overdue, got %+v", filtered)
	}
}

// TestThreeWayAgreement_TaskDueEarlierToday is the Task 1 regression test:
// "tl ls --due today", "tl ls --view today" and the agenda must agree about
// a task due 09:00 today, queried at 15:00 (deadline already passed) - and
// so must their "overdue" counterparts. --due goes through dueRangeLabels,
// --view through resolveViewSpec's DueBuckets; both end up filtered by the
// same filterByDueBucket/dueBucketOf predicate the agenda groups by, so
// there is exactly one definition of "today" rather than one per surface.
func TestThreeWayAgreement_TaskDueEarlierToday(t *testing.T) {
	now := time.Date(2026, 8, 12, 15, 0, 0, 0, time.Local)
	task := Task{Name: "standup", DueAt: sql.NullTime{Time: dt(2026, 8, 12, 9, 0), Valid: true}, DueHasTime: true}

	agendaBuckets := bucketAgenda([]Task{task}, now)
	if len(agendaBuckets) != 1 || agendaBuckets[0].Label != "Overdue" {
		t.Fatalf("agenda: expected a single Overdue bucket, got %+v", agendaBuckets)
	}

	todayCLILabels, err := dueRangeLabels("today")
	if err != nil {
		t.Fatalf("dueRangeLabels(today): %v", err)
	}
	if got := filterByDueBucket([]Task{task}, now, todayCLILabels); len(got) != 0 {
		t.Errorf("--due today: expected the task excluded, got %+v", got)
	}

	todayViewQ, err := resolveViewSpec(ViewSpec{Due: "today"}, now, nil, nil)
	if err != nil {
		t.Fatalf("resolveViewSpec(today): %v", err)
	}
	if got := filterByDueBucket([]Task{task}, now, bucketSet(todayViewQ.DueBuckets)); len(got) != 0 {
		t.Errorf("--view today: expected the task excluded, got %+v", got)
	}

	overdueCLILabels, err := dueRangeLabels("overdue")
	if err != nil {
		t.Fatalf("dueRangeLabels(overdue): %v", err)
	}
	if got := filterByDueBucket([]Task{task}, now, overdueCLILabels); len(got) != 1 {
		t.Errorf("--due overdue: expected the task included, got %+v", got)
	}

	overdueViewQ, err := resolveViewSpec(ViewSpec{Due: "overdue"}, now, nil, nil)
	if err != nil {
		t.Fatalf("resolveViewSpec(overdue): %v", err)
	}
	if got := filterByDueBucket([]Task{task}, now, bucketSet(overdueViewQ.DueBuckets)); len(got) != 1 {
		t.Errorf("--view overdue: expected the task included, got %+v", got)
	}
}

func TestFlattenAgendaTasks(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local)
	tasks := []Task{
		{Name: "someday-1"},
		{Name: "later-1", DueAt: sql.NullTime{Time: dt(2026, 8, 20, 0, 0), Valid: true}},
		{Name: "today-1", DueAt: sql.NullTime{Time: dt(2026, 8, 12, 0, 0), Valid: true}},
	}
	flat := flattenAgendaTasks(bucketAgenda(tasks, now))
	if len(flat) != 3 {
		t.Fatalf("expected 3 flattened tasks, got %d", len(flat))
	}
	// Today bucket precedes Later precedes Someday.
	want := []string{"today-1", "later-1", "someday-1"}
	var got []string
	for _, tsk := range flat {
		got = append(got, tsk.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("flattened order = %v, want %v", got, want)
	}
}
