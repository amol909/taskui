package main

import (
	"fmt"
	"sort"
	"time"
)

// agendaBucket is one heading's worth of tasks in the agenda view/CLI
// listing, in display order.
type agendaBucket struct {
	Label string
	Tasks []Task
}

// agendaBucketLabels is the fixed bucket order; bucketAgenda skips any that
// end up empty.
var agendaBucketLabels = []string{"Overdue", "Today", "Tomorrow", "This week", "Later", "Someday"}

// bucketAgenda groups tasks into the agenda's fixed set of local-time
// buckets, skipping any that end up empty. Within a bucket, tasks are
// sorted by due time then priority descending. now anchors every boundary -
// bucketAgenda never calls time.Now() itself, so it stays as deterministic
// as ParseTask/RecurRule.Next.
//
// A date-only task (DueHasTime false) is only overdue once the *end* of its
// due day has passed, matching formatDueBadge's rule; a task with a time is
// overdue as soon as that exact instant passes, even on the same day.
func bucketAgenda(tasks []Task, now time.Time) []agendaBucket {
	grouped := make(map[string][]Task, len(agendaBucketLabels))

	for _, t := range tasks {
		label := dueBucketOf(t, now).Label
		grouped[label] = append(grouped[label], t)
	}

	var buckets []agendaBucket
	for _, label := range agendaBucketLabels {
		ts := grouped[label]
		if len(ts) == 0 {
			continue
		}
		sort.SliceStable(ts, func(i, j int) bool {
			ti, tj := time.Time{}, time.Time{}
			if ts[i].DueAt.Valid {
				ti = ts[i].DueAt.Time
			}
			if ts[j].DueAt.Valid {
				tj = ts[j].DueAt.Time
			}
			if !ti.Equal(tj) {
				return ti.Before(tj)
			}
			return ts[i].Priority > ts[j].Priority
		})
		buckets = append(buckets, agendaBucket{Label: label, Tasks: ts})
	}
	return buckets
}

// dueBucketOf returns the agenda bucket a single task falls into, using
// exactly the rule bucketAgenda groups by: a date-only task (DueHasTime
// false) is only overdue once the *end* of its due day has passed, matching
// formatDueBadge; a task with a time is overdue as soon as that exact
// instant passes, even on the same day. It returns an agendaBucket with only
// Label set (Tasks is unused for a single task) so bucketAgenda and other
// callers - "tl ls --due", saved views with a Due filter - can share one
// predicate instead of drifting apart the way bucketAgenda and the old
// --due midnight-boundary comparison did (see Task 0b).
func dueBucketOf(t Task, now time.Time) agendaBucket {
	if !t.DueAt.Valid {
		return agendaBucket{Label: "Someday"}
	}

	today := dateOnly(now)
	tomorrow := today.AddDate(0, 0, 1)
	daysToSunday := (7 - int(today.Weekday())) % 7
	endOfWeek := today.AddDate(0, 0, daysToSunday)

	due := t.DueAt.Time
	dueDay := dateOnly(due)
	deadline := due
	if !t.DueHasTime {
		deadline = dueDay.AddDate(0, 0, 1)
	}

	switch {
	case now.After(deadline):
		return agendaBucket{Label: "Overdue"}
	case dueDay.Equal(today):
		return agendaBucket{Label: "Today"}
	case dueDay.Equal(tomorrow):
		return agendaBucket{Label: "Tomorrow"}
	case !dueDay.After(endOfWeek):
		return agendaBucket{Label: "This week"}
	default:
		return agendaBucket{Label: "Later"}
	}
}

// dueRangeLabels returns the set of agenda bucket labels that a "--due
// <value>" CLI flag or a saved view's ViewSpec.Due should include, pairing
// the coarse SQL TaskQuery bounds callers pre-filter with against
// dueBucketOf's precise per-task predicate. "week" spans today through the
// end of the current calendar week, so it covers three buckets.
func dueRangeLabels(due string) (map[string]bool, error) {
	switch due {
	case "today":
		return map[string]bool{"Today": true}, nil
	case "tomorrow":
		return map[string]bool{"Tomorrow": true}, nil
	case "week":
		return map[string]bool{"Today": true, "Tomorrow": true, "This week": true}, nil
	case "overdue":
		return map[string]bool{"Overdue": true}, nil
	default:
		return nil, fmt.Errorf("invalid due value %q (want today, tomorrow, week, or overdue)", due)
	}
}

// filterByDueBucket keeps only the tasks whose dueBucketOf label is in
// labels, preserving order. Used after a coarse SQL fetch to get exact
// agreement with the agenda's bucketing.
func filterByDueBucket(tasks []Task, now time.Time, labels map[string]bool) []Task {
	kept := tasks[:0]
	for _, t := range tasks {
		if labels[dueBucketOf(t, now).Label] {
			kept = append(kept, t)
		}
	}
	return kept
}

// flattenAgendaTasks concatenates every bucket's tasks in bucket order, for
// callers (the agenda view's cursor, the CLI's plain-text printer) that
// need one flat list alongside the grouping.
func flattenAgendaTasks(buckets []agendaBucket) []Task {
	var flat []Task
	for _, b := range buckets {
		flat = append(flat, b.Tasks...)
	}
	return flat
}
