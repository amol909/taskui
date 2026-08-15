package main

import (
	"testing"
	"time"
)

func TestParseRecur_SupportedPhrases(t *testing.T) {
	tests := []struct {
		input string
		want  RecurRule
	}{
		{"every day", RecurRule{Unit: "day", Interval: 1}},
		{"daily", RecurRule{Unit: "day", Interval: 1}},
		{"every 3 days", RecurRule{Unit: "day", Interval: 3}},
		{"every week", RecurRule{Unit: "week", Interval: 1}},
		{"weekly", RecurRule{Unit: "week", Interval: 1}},
		{"every 2 weeks", RecurRule{Unit: "week", Interval: 2}},
		{"every monday", RecurRule{Unit: "week", Interval: 1, Weekday: time.Monday, HasWeekday: true}},
		{"every other tuesday", RecurRule{Unit: "week", Interval: 2, Weekday: time.Tuesday, HasWeekday: true}},
		{"every month", RecurRule{Unit: "month", Interval: 1}},
		{"monthly", RecurRule{Unit: "month", Interval: 1}},
		{"every 6 months", RecurRule{Unit: "month", Interval: 6}},
		{"every year", RecurRule{Unit: "year", Interval: 1}},
		{"yearly", RecurRule{Unit: "year", Interval: 1}},
		{"last day of month", RecurRule{Unit: "month", Interval: 1, MonthDay: -1}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := ParseRecur(tt.input)
			if !ok {
				t.Fatalf("ParseRecur(%q) failed", tt.input)
			}
			if got != tt.want {
				t.Errorf("ParseRecur(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseRecur_CaseInsensitive(t *testing.T) {
	got, ok := ParseRecur("EVERY MONDAY")
	if !ok || got.Unit != "week" || !got.HasWeekday || got.Weekday != time.Monday {
		t.Fatalf("ParseRecur(EVERY MONDAY) = %+v, ok=%v", got, ok)
	}
}

func TestParseRecur_Rejects(t *testing.T) {
	tests := []string{"", "sometimes", "every blah", "every 0 days", "every -1 days", "every other blah", "every"}
	for _, s := range tests {
		if _, ok := ParseRecur(s); ok {
			t.Errorf("ParseRecur(%q) unexpectedly succeeded", s)
		}
	}
}

// TestRecurRule_RoundTrip is the main defence against ParseRecur and
// RecurRule.String drifting apart - same discipline as ParseTask/FormatTask.
func TestRecurRule_RoundTrip(t *testing.T) {
	phrases := []string{
		"every day", "every 3 days",
		"every week", "every 2 weeks",
		"every monday", "every other tuesday",
		"every month", "every 6 months",
		"every year",
		"last day of month",
	}
	for _, p := range phrases {
		t.Run(p, func(t *testing.T) {
			r1, ok := ParseRecur(p)
			if !ok {
				t.Fatalf("ParseRecur(%q) failed", p)
			}
			s := r1.String()
			r2, ok := ParseRecur(s)
			if !ok {
				t.Fatalf("ParseRecur(String()) failed for %q -> %q", p, s)
			}
			if r1 != r2 {
				t.Errorf("round trip mismatch: %q -> %+v -> %q -> %+v", p, r1, s, r2)
			}
		})
	}
}

func TestRecurRule_Next_Day(t *testing.T) {
	r := RecurRule{Unit: "day", Interval: 1}
	after := time.Date(2026, 8, 12, 9, 30, 0, 0, time.Local)
	want := time.Date(2026, 8, 13, 9, 30, 0, 0, time.Local)
	if got := r.Next(after); !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

func TestRecurRule_Next_NDays(t *testing.T) {
	r := RecurRule{Unit: "day", Interval: 3}
	after := time.Date(2026, 8, 12, 9, 30, 0, 0, time.Local)
	want := time.Date(2026, 8, 15, 9, 30, 0, 0, time.Local)
	if got := r.Next(after); !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

func TestRecurRule_Next_Week(t *testing.T) {
	r := RecurRule{Unit: "week", Interval: 1}
	after := time.Date(2026, 8, 12, 9, 30, 0, 0, time.Local) // Wednesday
	want := time.Date(2026, 8, 19, 9, 30, 0, 0, time.Local)
	if got := r.Next(after); !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

func TestRecurRule_Next_NWeeks(t *testing.T) {
	r := RecurRule{Unit: "week", Interval: 3}
	after := time.Date(2026, 8, 12, 9, 30, 0, 0, time.Local)
	want := time.Date(2026, 9, 2, 9, 30, 0, 0, time.Local)
	if got := r.Next(after); !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

func TestRecurRule_Next_Weekday(t *testing.T) {
	// after = Wednesday 12 Aug 2026; every monday -> next Monday, 17 Aug.
	r := RecurRule{Unit: "week", Interval: 1, Weekday: time.Monday, HasWeekday: true}
	after := time.Date(2026, 8, 12, 9, 30, 0, 0, time.Local)
	want := time.Date(2026, 8, 17, 9, 30, 0, 0, time.Local)
	if got := r.Next(after); !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

func TestRecurRule_Next_WeekdayStrictlyAfterSameDay(t *testing.T) {
	// after itself is a Monday; Next must move forward a full week, not
	// return the same instant.
	r := RecurRule{Unit: "week", Interval: 1, Weekday: time.Monday, HasWeekday: true}
	after := time.Date(2026, 8, 10, 9, 30, 0, 0, time.Local) // Monday
	want := time.Date(2026, 8, 17, 9, 30, 0, 0, time.Local)
	if got := r.Next(after); !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

func TestRecurRule_Next_EveryOtherWeekday(t *testing.T) {
	// after = Tuesday 11 Aug 2026; every other tuesday -> 2 tuesdays out, 25 Aug.
	r := RecurRule{Unit: "week", Interval: 2, Weekday: time.Tuesday, HasWeekday: true}
	after := time.Date(2026, 8, 11, 9, 30, 0, 0, time.Local) // Tuesday
	want := time.Date(2026, 8, 25, 9, 30, 0, 0, time.Local)
	if got := r.Next(after); !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

func TestRecurRule_Next_Month(t *testing.T) {
	r := RecurRule{Unit: "month", Interval: 1}
	after := time.Date(2026, 3, 15, 9, 0, 0, 0, time.Local)
	want := time.Date(2026, 4, 15, 9, 0, 0, 0, time.Local)
	if got := r.Next(after); !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

// TestRecurRule_Next_MonthEndClamping is the single most important test in
// this file: Go's time.Time.AddDate(0, 1, 0) turns 31 Jan into 3 Mar. Next
// must clamp to the last valid day of the target month instead.
func TestRecurRule_Next_MonthEndClamping(t *testing.T) {
	r := RecurRule{Unit: "month", Interval: 1}

	tests := []struct {
		name  string
		after time.Time
		want  time.Time
	}{
		{"31 Jan -> 28 Feb (non-leap year)", time.Date(2026, 1, 31, 10, 0, 0, 0, time.Local), time.Date(2026, 2, 28, 10, 0, 0, 0, time.Local)},
		{"31 Jan -> 29 Feb (leap year)", time.Date(2028, 1, 31, 10, 0, 0, 0, time.Local), time.Date(2028, 2, 29, 10, 0, 0, 0, time.Local)},
		{"31 Mar -> 30 Apr", time.Date(2026, 3, 31, 10, 0, 0, 0, time.Local), time.Date(2026, 4, 30, 10, 0, 0, 0, time.Local)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Next(tt.after)
			if !got.Equal(tt.want) {
				t.Errorf("Next(%v) = %v, want %v", tt.after, got, tt.want)
			}
		})
	}
}

func TestRecurRule_Next_MonthCrossesYearBoundary(t *testing.T) {
	r := RecurRule{Unit: "month", Interval: 2}
	after := time.Date(2026, 12, 15, 10, 0, 0, 0, time.Local)
	want := time.Date(2027, 2, 15, 10, 0, 0, 0, time.Local)
	if got := r.Next(after); !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

func TestRecurRule_Next_LastDayOfMonth(t *testing.T) {
	r := RecurRule{Unit: "month", Interval: 1, MonthDay: -1}

	tests := []struct {
		name  string
		after time.Time
		want  time.Time
	}{
		{"15 Jan -> 28 Feb (non-leap)", time.Date(2026, 1, 15, 10, 0, 0, 0, time.Local), time.Date(2026, 2, 28, 10, 0, 0, 0, time.Local)},
		{"15 Jan -> 29 Feb (leap year)", time.Date(2028, 1, 15, 10, 0, 0, 0, time.Local), time.Date(2028, 2, 29, 10, 0, 0, 0, time.Local)},
		{"30 Apr -> 31 May", time.Date(2026, 4, 30, 10, 0, 0, 0, time.Local), time.Date(2026, 5, 31, 10, 0, 0, 0, time.Local)},
		{"already on last day: 28 Feb -> 31 Mar", time.Date(2026, 2, 28, 10, 0, 0, 0, time.Local), time.Date(2026, 3, 31, 10, 0, 0, 0, time.Local)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Next(tt.after)
			if !got.Equal(tt.want) {
				t.Errorf("Next(%v) = %v, want %v", tt.after, got, tt.want)
			}
		})
	}
}

func TestRecurRule_Next_Year(t *testing.T) {
	r := RecurRule{Unit: "year", Interval: 1}
	after := time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local)
	want := time.Date(2027, 8, 12, 10, 0, 0, 0, time.Local)
	if got := r.Next(after); !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

func TestRecurRule_Next_YearLeapDayClamps(t *testing.T) {
	r := RecurRule{Unit: "year", Interval: 1}
	after := time.Date(2028, 2, 29, 10, 0, 0, 0, time.Local) // leap year
	want := time.Date(2029, 2, 28, 10, 0, 0, 0, time.Local)  // not a leap year
	if got := r.Next(after); !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

// TestRecurRule_First covers the Task 0 bug table (captured Thu 13 Aug
// 2026): First must land an anchored rule on the next point of its grid,
// not a full interval away like Next would, plus the two same-day edge
// cases where "now" already sits on the grid.
func TestRecurRule_First(t *testing.T) {
	now := time.Date(2026, 8, 13, 9, 0, 0, 0, time.Local) // Thursday

	tests := []struct {
		name string
		rule RecurRule
		now  time.Time
		want time.Time
	}{
		{
			name: "last day of month: Thu 13 Aug -> Mon 31 Aug (not Wed 30 Sep)",
			rule: RecurRule{Unit: "month", Interval: 1, MonthDay: -1},
			now:  now,
			want: time.Date(2026, 8, 31, 9, 0, 0, 0, time.Local),
		},
		{
			name: "every other tuesday: Thu 13 Aug -> Tue 18 Aug (not Tue 25 Aug)",
			rule: RecurRule{Unit: "week", Interval: 2, Weekday: time.Tuesday, HasWeekday: true},
			now:  now,
			want: time.Date(2026, 8, 18, 9, 0, 0, 0, time.Local),
		},
		{
			name: "every monday captured on a Monday -> that same Monday",
			rule: RecurRule{Unit: "week", Interval: 1, Weekday: time.Monday, HasWeekday: true},
			now:  time.Date(2026, 8, 10, 9, 0, 0, 0, time.Local), // Monday
			want: time.Date(2026, 8, 10, 9, 0, 0, 0, time.Local),
		},
		{
			name: "last day of month captured on the last day -> that same day",
			rule: RecurRule{Unit: "month", Interval: 1, MonthDay: -1},
			now:  time.Date(2026, 8, 31, 9, 0, 0, 0, time.Local),
			want: time.Date(2026, 8, 31, 9, 0, 0, 0, time.Local),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rule.First(tt.now); !got.Equal(tt.want) {
				t.Errorf("First(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

// TestRecurRule_First_UnanchoredFallsBackToNext proves every-day/every-N-
// days/every-month/every-year (no grid to land on) fall back to Next
// unchanged, rather than defaulting to "due immediately".
func TestRecurRule_First_UnanchoredFallsBackToNext(t *testing.T) {
	now := time.Date(2026, 8, 13, 9, 0, 0, 0, time.Local)

	rules := []RecurRule{
		{Unit: "day", Interval: 1},
		{Unit: "day", Interval: 3},
		{Unit: "month", Interval: 1},
		{Unit: "year", Interval: 1},
	}
	for _, r := range rules {
		if got, want := r.First(now), r.Next(now); !got.Equal(want) {
			t.Errorf("First(%v) for %+v = %v, want Next's %v", now, r, got, want)
		}
	}
}
