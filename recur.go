package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RecurRule is the parsed form of a recurrence. Zero value = no recurrence.
//
// Unit is one of "day", "week", "month", "year". Interval is always >= 1
// once returned from ParseRecur (Next treats < 1 as 1 defensively). Weekday
// / HasWeekday are only meaningful when Unit == "week" and the rule was
// written against a specific weekday ("every monday", "every other
// tuesday"). MonthDay is only meaningful when Unit == "month": 0 means
// "keep the day-of-month `after` was on, clamped to the target month's last
// day"; -1 means "always land on the target month's actual last day"
// ("last day of month").
type RecurRule struct {
	Unit       string // "day", "week", "month", "year"
	Interval   int    // >= 1
	Weekday    time.Weekday
	HasWeekday bool
	MonthDay   int // 0 = unset (keep day-of-month, clamped); -1 = last day of month
}

var (
	everyUnitRe         = regexp.MustCompile(`(?i)^every\s+(day|week|month|year)$`)
	everyNUnitRe        = regexp.MustCompile(`(?i)^every\s+(\d+)\s+(day|days|week|weeks|month|months)$`)
	everyOtherWeekdayRe = regexp.MustCompile(`(?i)^every\s+other\s+(` + weekdayPattern + `)$`)
	everyWeekdayRe      = regexp.MustCompile(`(?i)^every\s+(` + weekdayPattern + `)$`)
)

// ParseRecur parses the canonical recurrence phrases (case-insensitive):
//
//	every day / daily, every N days
//	every week / weekly, every N weeks
//	every <weekday>, every other <weekday>
//	every month / monthly, every N months
//	every year / yearly
//	last day of month
//
// Anything else is rejected (ok == false) - like ParseTask, an over-eager
// grammar here is worse than one that occasionally fails to recognise a
// phrase.
func ParseRecur(s string) (RecurRule, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return RecurRule{}, false
	}

	switch strings.ToLower(s) {
	case "daily":
		return RecurRule{Unit: "day", Interval: 1}, true
	case "weekly":
		return RecurRule{Unit: "week", Interval: 1}, true
	case "monthly":
		return RecurRule{Unit: "month", Interval: 1}, true
	case "yearly":
		return RecurRule{Unit: "year", Interval: 1}, true
	case "last day of month":
		return RecurRule{Unit: "month", Interval: 1, MonthDay: -1}, true
	}

	if m := everyOtherWeekdayRe.FindStringSubmatch(s); m != nil {
		wd, ok := weekdayByName[strings.ToLower(m[1])]
		if !ok {
			return RecurRule{}, false
		}
		return RecurRule{Unit: "week", Interval: 2, Weekday: wd, HasWeekday: true}, true
	}

	if m := everyWeekdayRe.FindStringSubmatch(s); m != nil {
		wd, ok := weekdayByName[strings.ToLower(m[1])]
		if !ok {
			return RecurRule{}, false
		}
		return RecurRule{Unit: "week", Interval: 1, Weekday: wd, HasWeekday: true}, true
	}

	if m := everyUnitRe.FindStringSubmatch(s); m != nil {
		return RecurRule{Unit: strings.ToLower(m[1]), Interval: 1}, true
	}

	if m := everyNUnitRe.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			return RecurRule{}, false
		}
		unit := strings.TrimSuffix(strings.ToLower(m[2]), "s")
		return RecurRule{Unit: unit, Interval: n}, true
	}

	return RecurRule{}, false
}

// String renders r back into the canonical text ParseRecur reads: this is
// what gets stored in tasks.recur_rule, so ParseRecur(r.String()) must
// always reproduce an equivalent rule - see the round-trip test in
// recur_test.go, same discipline as ParseTask/FormatTask.
func (r RecurRule) String() string {
	if r.Unit == "month" && r.MonthDay == -1 {
		return "last day of month"
	}

	switch r.Unit {
	case "day":
		if r.Interval <= 1 {
			return "every day"
		}
		return fmt.Sprintf("every %d days", r.Interval)
	case "week":
		if r.HasWeekday {
			if r.Interval == 2 {
				return "every other " + weekdayName(r.Weekday)
			}
			return "every " + weekdayName(r.Weekday)
		}
		if r.Interval <= 1 {
			return "every week"
		}
		return fmt.Sprintf("every %d weeks", r.Interval)
	case "month":
		if r.Interval <= 1 {
			return "every month"
		}
		return fmt.Sprintf("every %d months", r.Interval)
	case "year":
		return "every year"
	}
	return ""
}

// weekdayName renders wd in the lowercase full-name form used by both the
// canonical recurrence text and weekdayByName (parse.go).
func weekdayName(wd time.Weekday) string {
	return strings.ToLower(wd.String())
}

// Next returns the next occurrence strictly after `after`, preserving its
// clock time. Month and year arithmetic clamp to the target month's last
// valid day instead of overflowing into the following month the way
// time.Time.AddDate(0, 1, 0) does (31 Jan + 1 month must land on 28/29 Feb,
// not 3 Mar).
func (r RecurRule) Next(after time.Time) time.Time {
	interval := r.Interval
	if interval < 1 {
		interval = 1
	}

	switch r.Unit {
	case "day":
		return after.AddDate(0, 0, interval)
	case "week":
		if r.HasWeekday {
			next := nextWeekdayStrictlyAfter(after, r.Weekday)
			if interval > 1 {
				next = next.AddDate(0, 0, 7*(interval-1))
			}
			return next
		}
		return after.AddDate(0, 0, 7*interval)
	case "month":
		return addMonthsClamped(after, interval, r.MonthDay)
	case "year":
		return addYearsClamped(after, interval)
	default:
		return after
	}
}

// nextWeekdayStrictlyAfter returns the next occurrence of wd strictly after
// t (i.e. always moves forward at least one day, even when t already falls
// on wd), preserving t's clock time.
func nextWeekdayStrictlyAfter(t time.Time, wd time.Weekday) time.Time {
	diff := (int(wd) - int(t.Weekday()) + 7) % 7
	if diff == 0 {
		diff = 7
	}
	return t.AddDate(0, 0, diff)
}

// nextWeekdayOnOrAfter returns the next occurrence of wd on or after t (t
// itself, when t already falls on wd), preserving t's clock time. This is
// First's building block; Next uses nextWeekdayStrictlyAfter instead because
// subsequent occurrences must always move forward.
func nextWeekdayOnOrAfter(t time.Time, wd time.Weekday) time.Time {
	diff := (int(wd) - int(t.Weekday()) + 7) % 7
	return t.AddDate(0, 0, diff)
}

// First returns the first occurrence of a newly created recurring task, at
// or after now. Anchored rules (weekday, last-day-of-month) describe a grid
// of dates and land on the next point of that grid - which may be much
// sooner than a full interval away, unlike Next. Unanchored interval rules
// (every day, every N days, every month, every year) have no such grid to
// land on, so they fall back to Next unchanged: defaulting them to "due
// immediately" would be worse than one interval out.
func (r RecurRule) First(now time.Time) time.Time {
	switch {
	case r.HasWeekday:
		// Interval only governs later occurrences (every other tuesday
		// still starts on the very next tuesday), so it is ignored here.
		return nextWeekdayOnOrAfter(now, r.Weekday)

	case r.Unit == "month" && r.MonthDay == -1:
		lastDay := lastDayOfMonth(now.Year(), now.Month())
		candidate := time.Date(now.Year(), now.Month(), lastDay, now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), now.Location())
		if !candidate.Before(now) {
			return candidate
		}
		return addMonthsClamped(now, 1, -1)

	default:
		return r.Next(now)
	}
}

// addMonthsClamped advances t by `months` months, preserving clock time and
// clamping the day-of-month to the target month's last valid day rather
// than overflowing (Go's AddDate(0,1,0) turns 31 Jan into 3 Mar). When
// monthDay == -1, the result always lands on the target month's actual last
// day, regardless of t's original day-of-month.
func addMonthsClamped(t time.Time, months int, monthDay int) time.Time {
	y, m, d := t.Date()
	h, mi, s := t.Clock()
	ns := t.Nanosecond()
	loc := t.Location()

	totalMonths := int(m) - 1 + months
	targetYear := y + totalMonths/12
	targetMonthIdx := totalMonths % 12
	if targetMonthIdx < 0 {
		targetMonthIdx += 12
		targetYear--
	}
	targetMonth := time.Month(targetMonthIdx + 1)

	lastDay := lastDayOfMonth(targetYear, targetMonth)

	day := d
	if monthDay == -1 {
		day = lastDay
	} else if monthDay > 0 {
		day = monthDay
	}
	if day > lastDay {
		day = lastDay
	}

	return time.Date(targetYear, targetMonth, day, h, mi, s, ns, loc)
}

// addYearsClamped advances t by `years` years, preserving clock time and
// clamping 29 Feb to 28 Feb in a target year that isn't a leap year.
func addYearsClamped(t time.Time, years int) time.Time {
	y, m, d := t.Date()
	h, mi, s := t.Clock()
	ns := t.Nanosecond()
	loc := t.Location()

	targetYear := y + years
	lastDay := lastDayOfMonth(targetYear, m)
	day := d
	if day > lastDay {
		day = lastDay
	}

	return time.Date(targetYear, m, day, h, mi, s, ns, loc)
}

// lastDayOfMonth returns the number of days in month m of year y. Day 0 of
// the following month is, by time.Date's normalisation rules, the last day
// of month m.
func lastDayOfMonth(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
