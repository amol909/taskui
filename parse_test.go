package main

import (
	"database/sql"
	"testing"
	"time"
)

// testNow anchors every parser test. It is a Wednesday so weekday arithmetic
// is exercised in both directions (a weekday earlier in the week and one
// later in the week).
var testNow = time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local)

func dt(y, mo, d, h, mi int) time.Time {
	return time.Date(y, time.Month(mo), d, h, mi, 0, 0, time.Local)
}

func TestParseTask_DateForms(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantDue     time.Time
		wantHasTime bool
	}{
		{"today", "task today", dt(2026, 8, 12, 0, 0), false},
		{"tonight", "task tonight", dt(2026, 8, 12, 20, 0), true},
		{"tomorrow", "task tomorrow", dt(2026, 8, 13, 0, 0), false},
		{"tmr", "task tmr", dt(2026, 8, 13, 0, 0), false},
		{"tmrw", "task tmrw", dt(2026, 8, 13, 0, 0), false},
		{"yesterday", "task yesterday", dt(2026, 8, 11, 0, 0), false},
		{"eod", "task eod", dt(2026, 8, 12, 18, 0), true},
		{"eow", "task eow", dt(2026, 8, 14, 0, 0), false},
		{"next week", "task next week", dt(2026, 8, 19, 0, 0), false},
		{"next month", "task next month", dt(2026, 9, 12, 0, 0), false},
		{"bare weekday equals today", "task wednesday", dt(2026, 8, 12, 0, 0), false},
		{"bare weekday later this week", "task friday", dt(2026, 8, 14, 0, 0), false},
		{"bare weekday abbreviation", "task fri", dt(2026, 8, 14, 0, 0), false},
		{"next weekday", "task next friday", dt(2026, 8, 21, 0, 0), false},
		{"next weekday equal to today", "task next wednesday", dt(2026, 8, 19, 0, 0), false},
		{"in N days", "task in 3 days", dt(2026, 8, 15, 0, 0), false},
		{"in 1 day singular", "task in 1 day", dt(2026, 8, 13, 0, 0), false},
		{"in N weeks", "task in 1 week", dt(2026, 8, 19, 0, 0), false},
		{"in N months", "task in 2 months", dt(2026, 10, 12, 0, 0), false},
		{"iso date", "task 2026-09-01", dt(2026, 9, 1, 0, 0), false},
		{"dd/mm day first", "task 14/08", dt(2026, 8, 14, 0, 0), false},
		{"dd/mm/yyyy day first", "task 14/08/2027", dt(2027, 8, 14, 0, 0), false},
		{"month day abbrev", "task aug 20", dt(2026, 8, 20, 0, 0), false},
		{"day month abbrev", "task 20 aug", dt(2026, 8, 20, 0, 0), false},
		{"month day full name", "task august 20", dt(2026, 8, 20, 0, 0), false},
		{"day month full name", "task 20 august", dt(2026, 8, 20, 0, 0), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ParseTask(tt.input, testNow)
			if p.DueAt == nil {
				t.Fatalf("%s: expected a due date, got nil", tt.input)
			}
			if !p.DueAt.Equal(tt.wantDue) {
				t.Errorf("%s: DueAt = %v, want %v", tt.input, p.DueAt, tt.wantDue)
			}
			if p.DueHasTime != tt.wantHasTime {
				t.Errorf("%s: DueHasTime = %v, want %v", tt.input, p.DueHasTime, tt.wantHasTime)
			}
		})
	}
}

func TestParseTask_TimeForms(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantDue time.Time
	}{
		{"5pm bare, applies to today", "task 5pm", dt(2026, 8, 12, 17, 0)},
		{"5.30pm", "task 5.30pm", dt(2026, 8, 12, 17, 30)},
		{"5:30pm", "task 5:30pm", dt(2026, 8, 12, 17, 30)},
		{"11am", "task 11am", dt(2026, 8, 12, 11, 0)},
		{"24-hour", "task 17:00", dt(2026, 8, 12, 17, 0)},
		{"24-hour zero padded", "task 09:30", dt(2026, 8, 12, 9, 30)},
		{"noon", "task noon", dt(2026, 8, 12, 12, 0)},
		{"midnight rolls to next day", "task midnight", dt(2026, 8, 13, 0, 0)},
		{"leading at absorbed", "task at 5pm", dt(2026, 8, 12, 17, 0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ParseTask(tt.input, testNow)
			if p.DueAt == nil {
				t.Fatalf("%s: expected a due date, got nil", tt.input)
			}
			if !p.DueAt.Equal(tt.wantDue) {
				t.Errorf("%s: DueAt = %v, want %v", tt.input, p.DueAt, tt.wantDue)
			}
			if !p.DueHasTime {
				t.Errorf("%s: expected DueHasTime true", tt.input)
			}
		})
	}
}

func TestParseTask_TomorrowWithTime(t *testing.T) {
	p := ParseTask("task tomorrow 5pm", testNow)
	want := dt(2026, 8, 13, 17, 0)
	if p.DueAt == nil || !p.DueAt.Equal(want) {
		t.Fatalf("DueAt = %v, want %v", p.DueAt, want)
	}
	if !p.DueHasTime {
		t.Errorf("expected DueHasTime true for 'tomorrow 5pm'")
	}
}

func TestParseTask_TomorrowAloneHasNoTime(t *testing.T) {
	p := ParseTask("task tomorrow", testNow)
	want := dt(2026, 8, 13, 0, 0)
	if p.DueAt == nil || !p.DueAt.Equal(want) {
		t.Fatalf("DueAt = %v, want %v (midnight)", p.DueAt, want)
	}
	if p.DueHasTime {
		t.Errorf("expected DueHasTime false for 'tomorrow' alone")
	}
}

func TestParseTask_Category(t *testing.T) {
	p := ParseTask("task #work", testNow)
	if p.Category != "work" {
		t.Errorf("Category = %q, want %q", p.Category, "work")
	}
}

func TestParseTask_CategoryLastWins(t *testing.T) {
	p := ParseTask("task #work #home", testNow)
	if p.Category != "home" {
		t.Errorf("Category = %q, want %q", p.Category, "home")
	}
}

func TestParseTask_PriorityForms(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"task !high", PriorityHigh},
		{"task !h", PriorityHigh},
		{"task !med", PriorityMed},
		{"task !medium", PriorityMed},
		{"task !m", PriorityMed},
		{"task !low", PriorityLow},
		{"task !l", PriorityLow},
		{"task !1", PriorityLow},
		{"task !2", PriorityMed},
		{"task !3", PriorityHigh},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			p := ParseTask(tt.input, testNow)
			if p.Priority != tt.want {
				t.Errorf("Priority = %d, want %d", p.Priority, tt.want)
			}
		})
	}
}

func TestParseTask_PriorityLastWins(t *testing.T) {
	p := ParseTask("task !low !high", testNow)
	if p.Priority != PriorityHigh {
		t.Errorf("Priority = %d, want PriorityHigh", p.Priority)
	}
}

func TestParseTask_StatusForms(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"task @todo", "todo"},
		{"task @in-progress", "in-progress"},
		{"task @inprogress", "in-progress"},
		{"task @blocked", "blocked"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			p := ParseTask(tt.input, testNow)
			if p.Status != tt.want {
				t.Errorf("Status = %q, want %q", p.Status, tt.want)
			}
		})
	}
}

func TestParseTask_StatusLastWins(t *testing.T) {
	p := ParseTask("task @todo @blocked", testNow)
	if p.Status != "blocked" {
		t.Errorf("Status = %q, want %q", p.Status, "blocked")
	}
}

// TestParseTask_NegativeCases covers text that must NOT be swallowed by the
// grammar - these matter more than the positive cases, since an over-eager
// parser is worse than one that occasionally misses a date.
func TestParseTask_NegativeCases(t *testing.T) {
	t.Run("read chapter 3 - bare number is never a date", func(t *testing.T) {
		p := ParseTask("read chapter 3", testNow)
		if p.Name != "read chapter 3" {
			t.Errorf("Name = %q, want %q", p.Name, "read chapter 3")
		}
		if p.DueAt != nil {
			t.Errorf("expected no due date, got %v", p.DueAt)
		}
	})

	t.Run("fix issue #42 today - #42 is a valid category, today still parses", func(t *testing.T) {
		p := ParseTask("fix issue #42 today", testNow)
		if p.Category != "42" {
			t.Errorf("Category = %q, want %q", p.Category, "42")
		}
		want := dt(2026, 8, 12, 0, 0)
		if p.DueAt == nil || !p.DueAt.Equal(want) {
			t.Errorf("DueAt = %v, want %v (today)", p.DueAt, want)
		}
	})

	t.Run("score was 3:1 - not a 24-hour time", func(t *testing.T) {
		p := ParseTask("score was 3:1", testNow)
		if p.Name != "score was 3:1" {
			t.Errorf("Name = %q, want %q", p.Name, "score was 3:1")
		}
		if p.DueAt != nil {
			t.Errorf("expected no due date, got %v", p.DueAt)
		}
	})

	t.Run("email c# - trailing bare # is not a category", func(t *testing.T) {
		p := ParseTask("email c#", testNow)
		if p.Name != "email c#" {
			t.Errorf("Name = %q, want %q", p.Name, "email c#")
		}
		if p.Category != "" {
			t.Errorf("Category = %q, want none", p.Category)
		}
	})

	t.Run("meeting at the office - bare at with no time is not eaten", func(t *testing.T) {
		p := ParseTask("meeting at the office", testNow)
		if p.Name != "meeting at the office" {
			t.Errorf("Name = %q, want %q", p.Name, "meeting at the office")
		}
		if p.DueAt != nil {
			t.Errorf("expected no due date, got %v", p.DueAt)
		}
	})
}

func TestParseTask_NameReconstruction(t *testing.T) {
	p := ParseTask("pay rent tomorrow 5pm #finance !high", testNow)
	if p.Name != "pay rent" {
		t.Errorf("Name = %q, want %q", p.Name, "pay rent")
	}
}

func TestParseTask_NameReconstruction_TokenInMiddle(t *testing.T) {
	p := ParseTask("call #work mum tomorrow", testNow)
	if p.Name != "call mum" {
		t.Errorf("Name = %q, want %q", p.Name, "call mum")
	}
}

func TestParseTask_NonASCII(t *testing.T) {
	input := "café tomorrow"
	p := ParseTask(input, testNow)
	if p.Name != "café" {
		t.Errorf("Name = %q, want %q", p.Name, "café")
	}
	for _, tok := range p.Tokens {
		if tok.Start < 0 || tok.End > len(input) || tok.Start > tok.End {
			t.Errorf("invalid token offsets: %+v (input length %d)", tok, len(input))
		}
	}
}

func TestParseTask_TokensInInputOrder(t *testing.T) {
	p := ParseTask("call #work mum tomorrow !high", testNow)
	if len(p.Tokens) != 3 {
		t.Fatalf("expected 3 tokens (#work, tomorrow, !high), got %d: %+v", len(p.Tokens), p.Tokens)
	}
	for i := 1; i < len(p.Tokens); i++ {
		if p.Tokens[i].Start < p.Tokens[i-1].Start {
			t.Errorf("tokens not in input order: %+v", p.Tokens)
		}
	}
}

// TestParseTask_RecurrenceBeatsBareWeekday proves the ordering requirement:
// "every monday" must be consumed as a recurrence token before the
// bare-weekday date rule gets a chance to claim "monday" out of it. Since a
// recurring task needs a first occurrence, DueAt still ends up set - to the
// next Monday.
func TestParseTask_RecurrenceBeatsBareWeekday(t *testing.T) {
	p := ParseTask("standup every monday", testNow)
	if p.Name != "standup" {
		t.Errorf("Name = %q, want %q", p.Name, "standup")
	}
	if p.Recur != "every monday" {
		t.Errorf("Recur = %q, want %q", p.Recur, "every monday")
	}
	want := dt(2026, 8, 17, 0, 0) // testNow is Wed 12 Aug 2026; next Monday is 17 Aug
	if p.DueAt == nil || !p.DueAt.Equal(want) {
		t.Errorf("DueAt = %v, want %v (next Monday)", p.DueAt, want)
	}
}

func TestParseTask_RecurrencePhrases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gym every day", "every day"},
		{"gym daily", "every day"},
		{"gym every 3 days", "every 3 days"},
		{"gym every week", "every week"},
		{"gym weekly", "every week"},
		{"gym every 2 weeks", "every 2 weeks"},
		{"gym every other tuesday", "every other tuesday"},
		{"gym every month", "every month"},
		{"gym monthly", "every month"},
		{"gym every year", "every year"},
		{"gym yearly", "every year"},
		{"gym last day of month", "last day of month"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			p := ParseTask(tt.input, testNow)
			if p.Recur != tt.want {
				t.Errorf("Recur = %q, want %q", p.Recur, tt.want)
			}
			if p.Name != "gym" {
				t.Errorf("Name = %q, want %q", p.Name, "gym")
			}
		})
	}
}

// TestRoundTrip_Recurrence proves FormatTask emits the recurrence back in a
// form ParseTask reads identically - same discipline as the Category/
// Priority/Status/DueAt round-trip below.
func TestRoundTrip_Recurrence(t *testing.T) {
	tasks := []Task{
		{Name: "standup", RecurRule: "every monday", Status: "todo"},
		{Name: "invoice", RecurRule: "last day of month", Status: "todo"},
		{Name: "backup", RecurRule: "every 3 days", Status: "todo"},
		{Name: "one-off", Status: "todo"},
	}
	for _, task := range tasks {
		t.Run(task.Name, func(t *testing.T) {
			formatted := FormatTask(task)
			got := ParseTask(formatted, testNow)
			if got.Recur != task.RecurRule {
				t.Errorf("Recur = %q, want %q (formatted: %q)", got.Recur, task.RecurRule, formatted)
			}
		})
	}
}

// TestRoundTrip_ParseFormat is the main defence against ParseTask and
// FormatTask drifting apart: whatever FormatTask emits for a task must
// parse back into the same Category, Priority, Status, DueAt and
// DueHasTime.
func TestRoundTrip_ParseFormat(t *testing.T) {
	dueDateOnly := dt(2026, 8, 20, 0, 0)
	dueWithTime := dt(2026, 8, 20, 17, 0)

	tasks := []Task{
		{Name: "buy milk"},
		{Name: "pay rent", CategoryName: "finance", Priority: PriorityHigh, Status: "todo"},
		{
			Name: "call mum", CategoryName: "personal", Priority: PriorityMed, Status: "in-progress",
			DueAt: sql.NullTime{Time: dueDateOnly, Valid: true}, DueHasTime: false,
		},
		{
			Name: "ship release", CategoryName: "work", Priority: PriorityLow, Status: "blocked",
			DueAt: sql.NullTime{Time: dueWithTime, Valid: true}, DueHasTime: true,
		},
		{Name: "no frills task", Status: "todo"},
	}

	for _, task := range tasks {
		t.Run(task.Name, func(t *testing.T) {
			formatted := FormatTask(task)
			got := ParseTask(formatted, testNow)

			if got.Category != task.CategoryName {
				t.Errorf("Category = %q, want %q (formatted: %q)", got.Category, task.CategoryName, formatted)
			}
			if got.Priority != task.Priority {
				t.Errorf("Priority = %d, want %d (formatted: %q)", got.Priority, task.Priority, formatted)
			}

			wantStatus := task.Status
			if wantStatus == "" {
				wantStatus = "todo"
			}
			if got.Status != wantStatus {
				t.Errorf("Status = %q, want %q (formatted: %q)", got.Status, wantStatus, formatted)
			}

			if task.DueAt.Valid {
				if got.DueAt == nil || !got.DueAt.Equal(task.DueAt.Time) {
					t.Errorf("DueAt = %v, want %v (formatted: %q)", got.DueAt, task.DueAt.Time, formatted)
				}
			} else if got.DueAt != nil {
				t.Errorf("DueAt = %v, want nil (formatted: %q)", got.DueAt, formatted)
			}

			if got.DueHasTime != task.DueHasTime {
				t.Errorf("DueHasTime = %v, want %v (formatted: %q)", got.DueHasTime, task.DueHasTime, formatted)
			}
		})
	}
}
