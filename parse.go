package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TokenKind identifies what a recognised span of the capture input means.
type TokenKind int

const (
	TokenText TokenKind = iota
	TokenCategory
	TokenPriority
	TokenStatus
	TokenDue
	TokenRecur
)

// Token is a recognised span of the original input, given as byte offsets so
// the UI can highlight what was understood without re-running the parser.
type Token struct {
	Start, End int // byte offsets into the original input
	Kind       TokenKind
}

// ParsedTask is the result of parsing a single line of natural-language
// capture input. Category, Priority, Status and DueAt reflect what was
// found; Name is the input with every recognised token removed.
type ParsedTask struct {
	Name       string
	Category   string // "" = none
	Priority   int
	Status     string // "" = leave default
	DueAt      *time.Time
	DueHasTime bool
	Recur      string  // canonical recurrence text ("" = none), see recur.go
	Tokens     []Token // every recognised span, in input order
}

// span is a byte-offset range, used internally to detect overlap between
// candidate matches (e.g. a weekday name that also appears inside a
// "next <weekday>" match, or a date word swallowed by an adjacent #category).
type span struct{ start, end int }

func (s span) overlaps(o span) bool {
	return s.start < o.end && o.start < s.end
}

func overlapsAny(s span, excl []span) bool {
	for _, e := range excl {
		if s.overlaps(e) {
			return true
		}
	}
	return false
}

// ---- grammar: category / priority / status ----

var (
	categoryRe = regexp.MustCompile(`#[A-Za-z0-9_-]+`)
	priorityRe = regexp.MustCompile(`(?i)!(?:high|medium|med|h|m|low|l|1|2|3)\b`)
	statusRe   = regexp.MustCompile(`(?i)@(?:in-progress|inprogress|todo|blocked)\b`)
)

func priorityFromWord(word string) int {
	switch strings.ToLower(word) {
	case "high", "h":
		return PriorityHigh
	case "medium", "med", "m":
		return PriorityMed
	case "low", "l":
		return PriorityLow
	case "1":
		return PriorityLow
	case "2":
		return PriorityMed
	case "3":
		return PriorityHigh
	}
	return PriorityNone
}

func statusFromWord(word string) string {
	switch strings.ToLower(word) {
	case "todo":
		return "todo"
	case "in-progress", "inprogress":
		return "in-progress"
	case "blocked":
		return "blocked"
	}
	return ""
}

// ---- grammar: dates ----

const weekdayPattern = `(?:monday|mon|tuesday|tue|wednesday|wed|thursday|thu|friday|fri|saturday|sat|sunday|sun)`
const monthPattern = `(?:january|jan|february|feb|march|mar|april|apr|may|june|jun|july|jul|august|aug|september|sep|october|oct|november|nov|december|dec)`

var weekdayByName = map[string]time.Weekday{
	"monday": time.Monday, "mon": time.Monday,
	"tuesday": time.Tuesday, "tue": time.Tuesday,
	"wednesday": time.Wednesday, "wed": time.Wednesday,
	"thursday": time.Thursday, "thu": time.Thursday,
	"friday": time.Friday, "fri": time.Friday,
	"saturday": time.Saturday, "sat": time.Saturday,
	"sunday": time.Sunday, "sun": time.Sunday,
}

var monthByName = map[string]time.Month{
	"jan": time.January, "january": time.January,
	"feb": time.February, "february": time.February,
	"mar": time.March, "march": time.March,
	"apr": time.April, "april": time.April,
	"may": time.May,
	"jun": time.June, "june": time.June,
	"jul": time.July, "july": time.July,
	"aug": time.August, "august": time.August,
	"sep": time.September, "september": time.September,
	"oct": time.October, "october": time.October,
	"nov": time.November, "november": time.November,
	"dec": time.December, "december": time.December,
}

// dateOnly strips the time-of-day, keeping the date's location.
func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// nextWeekdayIncludingToday returns the next occurrence of wd, treating
// "today" as a match if now already falls on wd.
func nextWeekdayIncludingToday(now time.Time, wd time.Weekday) time.Time {
	base := dateOnly(now)
	diff := (int(wd) - int(base.Weekday()) + 7) % 7
	return base.AddDate(0, 0, diff)
}

// groupText returns the text of capturing group groupIdx (1-based, as in
// regexp group numbering) from a FindAllStringSubmatchIndex loc, and
// whether that group participated in the match.
func groupText(input string, loc []int, groupIdx int) (string, bool) {
	i := groupIdx * 2
	if i+1 >= len(loc) {
		return "", false
	}
	start, end := loc[i], loc[i+1]
	if start < 0 || end < 0 {
		return "", false
	}
	return input[start:end], true
}

type dateRule struct {
	re      *regexp.Regexp
	resolve func(now time.Time, input string, loc []int) (date time.Time, hasTime bool, ok bool)
}

var dateRules = []dateRule{
	{regexp.MustCompile(`(?i)\btonight\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		d := dateOnly(now)
		return time.Date(d.Year(), d.Month(), d.Day(), 20, 0, 0, 0, now.Location()), true, true
	}},
	{regexp.MustCompile(`(?i)\btoday\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		return dateOnly(now), false, true
	}},
	{regexp.MustCompile(`(?i)\b(?:tomorrow|tmrw|tmr)\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		return dateOnly(now).AddDate(0, 0, 1), false, true
	}},
	{regexp.MustCompile(`(?i)\byesterday\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		return dateOnly(now).AddDate(0, 0, -1), false, true
	}},
	{regexp.MustCompile(`(?i)\beod\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		d := dateOnly(now)
		return time.Date(d.Year(), d.Month(), d.Day(), 18, 0, 0, 0, now.Location()), true, true
	}},
	{regexp.MustCompile(`(?i)\beow\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		return nextWeekdayIncludingToday(now, time.Friday), false, true
	}},
	{regexp.MustCompile(`(?i)\bnext\s+week\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		return dateOnly(now).AddDate(0, 0, 7), false, true
	}},
	{regexp.MustCompile(`(?i)\bnext\s+month\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		return dateOnly(now).AddDate(0, 1, 0), false, true
	}},
	{regexp.MustCompile(`(?i)\bnext\s+(` + weekdayPattern + `)\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		word, ok := groupText(input, loc, 1)
		if !ok {
			return time.Time{}, false, false
		}
		wd, ok := weekdayByName[strings.ToLower(word)]
		if !ok {
			return time.Time{}, false, false
		}
		return nextWeekdayIncludingToday(now, wd).AddDate(0, 0, 7), false, true
	}},
	{regexp.MustCompile(`(?i)\b(` + weekdayPattern + `)\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		word, ok := groupText(input, loc, 1)
		if !ok {
			return time.Time{}, false, false
		}
		wd, ok := weekdayByName[strings.ToLower(word)]
		if !ok {
			return time.Time{}, false, false
		}
		return nextWeekdayIncludingToday(now, wd), false, true
	}},
	{regexp.MustCompile(`(?i)\bin\s+(\d+)\s+(day|days|week|weeks|month|months)\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		numStr, _ := groupText(input, loc, 1)
		unit, _ := groupText(input, loc, 2)
		n, err := strconv.Atoi(numStr)
		if err != nil {
			return time.Time{}, false, false
		}
		base := dateOnly(now)
		switch strings.TrimSuffix(strings.ToLower(unit), "s") {
		case "day":
			return base.AddDate(0, 0, n), false, true
		case "week":
			return base.AddDate(0, 0, 7*n), false, true
		case "month":
			return base.AddDate(0, n, 0), false, true
		}
		return time.Time{}, false, false
	}},
	{regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		yStr, _ := groupText(input, loc, 1)
		moStr, _ := groupText(input, loc, 2)
		dStr, _ := groupText(input, loc, 3)
		y, err1 := strconv.Atoi(yStr)
		mo, err2 := strconv.Atoi(moStr)
		d, err3 := strconv.Atoi(dStr)
		if err1 != nil || err2 != nil || err3 != nil || mo < 1 || mo > 12 || d < 1 || d > 31 {
			return time.Time{}, false, false
		}
		return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, now.Location()), false, true
	}},
	{regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})(?:/(\d{4}))?\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		// Day first: DD/MM or DD/MM/YYYY.
		dStr, _ := groupText(input, loc, 1)
		moStr, _ := groupText(input, loc, 2)
		d, err1 := strconv.Atoi(dStr)
		mo, err2 := strconv.Atoi(moStr)
		if err1 != nil || err2 != nil || mo < 1 || mo > 12 || d < 1 || d > 31 {
			return time.Time{}, false, false
		}
		year := now.Year()
		if yStr, ok := groupText(input, loc, 3); ok {
			if y, err := strconv.Atoi(yStr); err == nil {
				year = y
			}
		}
		return time.Date(year, time.Month(mo), d, 0, 0, 0, 0, now.Location()), false, true
	}},
	{regexp.MustCompile(`(?i)\b(` + monthPattern + `)\s+(\d{1,2})\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		word, _ := groupText(input, loc, 1)
		dStr, _ := groupText(input, loc, 2)
		mo, ok := monthByName[strings.ToLower(word)]
		d, err := strconv.Atoi(dStr)
		if !ok || err != nil || d < 1 || d > 31 {
			return time.Time{}, false, false
		}
		return time.Date(now.Year(), mo, d, 0, 0, 0, 0, now.Location()), false, true
	}},
	{regexp.MustCompile(`(?i)\b(\d{1,2})\s+(` + monthPattern + `)\b`), func(now time.Time, input string, loc []int) (time.Time, bool, bool) {
		dStr, _ := groupText(input, loc, 1)
		word, _ := groupText(input, loc, 2)
		mo, ok := monthByName[strings.ToLower(word)]
		d, err := strconv.Atoi(dStr)
		if !ok || err != nil || d < 1 || d > 31 {
			return time.Time{}, false, false
		}
		return time.Date(now.Year(), mo, d, 0, 0, 0, 0, now.Location()), false, true
	}},
}

// findDateToken scans the whole input for every date rule, discards any
// match overlapping excl, and returns the leftmost surviving match (ties
// broken by longer match). Only one date is ever recognised per input.
func findDateToken(input string, now time.Time, excl []span) (sp span, date time.Time, hasTime bool, found bool) {
	type candidate struct {
		sp      span
		date    time.Time
		hasTime bool
	}
	var candidates []candidate
	for _, rule := range dateRules {
		for _, loc := range rule.re.FindAllStringSubmatchIndex(input, -1) {
			s := span{loc[0], loc[1]}
			if overlapsAny(s, excl) {
				continue
			}
			d, ht, ok := rule.resolve(now, input, loc)
			if !ok {
				continue
			}
			candidates = append(candidates, candidate{s, d, ht})
		}
	}
	if len(candidates) == 0 {
		return span{}, time.Time{}, false, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].sp.start != candidates[j].sp.start {
			return candidates[i].sp.start < candidates[j].sp.start
		}
		return (candidates[i].sp.end - candidates[i].sp.start) > (candidates[j].sp.end - candidates[j].sp.start)
	})
	best := candidates[0]
	return best.sp, best.date, best.hasTime, true
}

// ---- grammar: times ----

type timeRule struct {
	re      *regexp.Regexp
	resolve func(input string, loc []int) (hour, minute, dayOffset int, ok bool)
}

var timeRules = []timeRule{
	// 5pm, 5.30pm, 5:30pm, 11am - optional leading "at " absorbed.
	{regexp.MustCompile(`(?i)\b(?:at\s+)?(\d{1,2})(?:[:.](\d{2}))?\s*([ap]m)\b`), func(input string, loc []int) (int, int, int, bool) {
		hourStr, _ := groupText(input, loc, 1)
		hour, err := strconv.Atoi(hourStr)
		if err != nil || hour < 1 || hour > 12 {
			return 0, 0, 0, false
		}
		minute := 0
		if minStr, ok := groupText(input, loc, 2); ok {
			minute, _ = strconv.Atoi(minStr)
		}
		ampm, _ := groupText(input, loc, 3)
		switch strings.ToLower(ampm) {
		case "pm":
			if hour != 12 {
				hour += 12
			}
		case "am":
			if hour == 12 {
				hour = 0
			}
		}
		return hour, minute, 0, true
	}},
	// 24-hour: 17:00, 09:30 - whole-word so "3:1" (score) never matches.
	{regexp.MustCompile(`(?i)\b(?:at\s+)?([01]?\d|2[0-3]):([0-5]\d)\b`), func(input string, loc []int) (int, int, int, bool) {
		hourStr, _ := groupText(input, loc, 1)
		minStr, _ := groupText(input, loc, 2)
		hour, err1 := strconv.Atoi(hourStr)
		minute, err2 := strconv.Atoi(minStr)
		if err1 != nil || err2 != nil {
			return 0, 0, 0, false
		}
		return hour, minute, 0, true
	}},
	{regexp.MustCompile(`(?i)\b(?:at\s+)?noon\b`), func(input string, loc []int) (int, int, int, bool) {
		return 12, 0, 0, true
	}},
	// midnight is the boundary between today and tomorrow: 00:00 next day.
	{regexp.MustCompile(`(?i)\b(?:at\s+)?midnight\b`), func(input string, loc []int) (int, int, int, bool) {
		return 0, 0, 1, true
	}},
}

// findTimeToken scans the whole input for every time rule, discards any
// match overlapping excl, and returns the leftmost surviving match.
func findTimeToken(input string, excl []span) (sp span, hour, minute, dayOffset int, found bool) {
	type candidate struct {
		sp                 span
		hour, minute, dOff int
	}
	var candidates []candidate
	for _, rule := range timeRules {
		for _, loc := range rule.re.FindAllStringSubmatchIndex(input, -1) {
			s := span{loc[0], loc[1]}
			if overlapsAny(s, excl) {
				continue
			}
			h, m, d, ok := rule.resolve(input, loc)
			if !ok {
				continue
			}
			candidates = append(candidates, candidate{s, h, m, d})
		}
	}
	if len(candidates) == 0 {
		return span{}, 0, 0, 0, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].sp.start != candidates[j].sp.start {
			return candidates[i].sp.start < candidates[j].sp.start
		}
		return (candidates[i].sp.end - candidates[i].sp.start) > (candidates[j].sp.end - candidates[j].sp.start)
	})
	best := candidates[0]
	return best.sp, best.hour, best.minute, best.dOff, true
}

// ---- grammar: recurrence ----

// recurFindRules scans the whole input for the Task 2 recurrence phrases
// (see recur.go's ParseRecur, which these mirror exactly). They must be
// tried, and their spans excluded from later matching, before
// findDateToken runs: "every monday" has to be consumed here so the
// bare-weekday date rule doesn't also claim "monday" out of it.
var recurFindRules = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\blast day of month\b`),
	regexp.MustCompile(`(?i)\bevery\s+other\s+` + weekdayPattern + `\b`),
	regexp.MustCompile(`(?i)\bevery\s+` + weekdayPattern + `\b`),
	regexp.MustCompile(`(?i)\bevery\s+\d+\s+(?:day|days|week|weeks|month|months)\b`),
	regexp.MustCompile(`(?i)\bevery\s+(?:day|week|month|year)\b`),
	regexp.MustCompile(`(?i)\bdaily\b`),
	regexp.MustCompile(`(?i)\bweekly\b`),
	regexp.MustCompile(`(?i)\bmonthly\b`),
	regexp.MustCompile(`(?i)\byearly\b`),
}

// findRecurToken scans the whole input for every recurrence rule, discards
// any match overlapping excl, and returns the leftmost surviving match
// (ties broken by longer match) - same discipline as findDateToken and
// findTimeToken.
func findRecurToken(input string, excl []span) (sp span, rule RecurRule, found bool) {
	type candidate struct {
		sp   span
		rule RecurRule
	}
	var candidates []candidate
	for _, re := range recurFindRules {
		for _, loc := range re.FindAllStringIndex(input, -1) {
			s := span{loc[0], loc[1]}
			if overlapsAny(s, excl) {
				continue
			}
			r, ok := ParseRecur(input[loc[0]:loc[1]])
			if !ok {
				continue
			}
			candidates = append(candidates, candidate{s, r})
		}
	}
	if len(candidates) == 0 {
		return span{}, RecurRule{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].sp.start != candidates[j].sp.start {
			return candidates[i].sp.start < candidates[j].sp.start
		}
		return (candidates[i].sp.end - candidates[i].sp.start) > (candidates[j].sp.end - candidates[j].sp.start)
	})
	best := candidates[0]
	return best.sp, best.rule, true
}

// ParseTask parses a single line of natural-language capture input into its
// name, category, priority, status and due date. now anchors every relative
// date/time computation - ParseTask never calls time.Now() itself, which is
// what keeps its tests deterministic.
//
// The grammar is deliberately narrow: only the forms documented on Token and
// in the readme are recognised. Anything else stays in Name verbatim - an
// over-eager parser that swallows words out of the name is worse than one
// that occasionally misses a date.
func ParseTask(input string, now time.Time) ParsedTask {
	var tokens []Token
	var excl []span

	category := ""
	for _, loc := range categoryRe.FindAllStringIndex(input, -1) {
		s := span{loc[0], loc[1]}
		excl = append(excl, s)
		category = input[loc[0]+1 : loc[1]]
		tokens = append(tokens, Token{Start: loc[0], End: loc[1], Kind: TokenCategory})
	}

	priority := PriorityNone
	for _, loc := range priorityRe.FindAllStringIndex(input, -1) {
		s := span{loc[0], loc[1]}
		excl = append(excl, s)
		priority = priorityFromWord(input[loc[0]+1 : loc[1]])
		tokens = append(tokens, Token{Start: loc[0], End: loc[1], Kind: TokenPriority})
	}

	status := ""
	for _, loc := range statusRe.FindAllStringIndex(input, -1) {
		s := span{loc[0], loc[1]}
		excl = append(excl, s)
		status = statusFromWord(input[loc[0]+1 : loc[1]])
		tokens = append(tokens, Token{Start: loc[0], End: loc[1], Kind: TokenStatus})
	}

	recurSpan, recurRule, recurFound := findRecurToken(input, excl)
	if recurFound {
		excl = append(excl, recurSpan)
		tokens = append(tokens, Token{Start: recurSpan.start, End: recurSpan.end, Kind: TokenRecur})
	}

	dateSpan, dateVal, dateHasTime, dateFound := findDateToken(input, now, excl)

	timeExcl := excl
	if dateFound {
		timeExcl = append(append([]span{}, excl...), dateSpan)
	}
	timeSpan, hour, minute, dayOffset, timeFound := findTimeToken(input, timeExcl)

	var dueAt *time.Time
	dueHasTime := false

	switch {
	case dateFound && timeFound:
		combined := time.Date(dateVal.Year(), dateVal.Month(), dateVal.Day()+dayOffset, hour, minute, 0, 0, now.Location())
		dueAt = &combined
		dueHasTime = true
		tokens = append(tokens, Token{Start: dateSpan.start, End: dateSpan.end, Kind: TokenDue})
		tokens = append(tokens, Token{Start: timeSpan.start, End: timeSpan.end, Kind: TokenDue})
	case dateFound:
		d := dateVal
		dueAt = &d
		dueHasTime = dateHasTime
		tokens = append(tokens, Token{Start: dateSpan.start, End: dateSpan.end, Kind: TokenDue})
	case timeFound:
		today := dateOnly(now)
		combined := time.Date(today.Year(), today.Month(), today.Day()+dayOffset, hour, minute, 0, 0, now.Location())
		dueAt = &combined
		dueHasTime = true
		tokens = append(tokens, Token{Start: timeSpan.start, End: timeSpan.end, Kind: TokenDue})
	case recurFound:
		// A recurring task needs a first occurrence, but no explicit
		// date/time was given - anchor it off now via the rule itself.
		// RecurRule.First lands anchored rules (weekday, last-day-of-month)
		// on the next point of their grid rather than a full interval out;
		// it preserves now's clock time the same way Next does, but this
		// due date is date-only (dueHasTime false), so drop the
		// time-of-day the same way the bare-weekday date rule does.
		d := dateOnly(recurRule.First(now))
		dueAt = &d
		dueHasTime = false
	}

	sort.Slice(tokens, func(i, j int) bool { return tokens[i].Start < tokens[j].Start })

	var b strings.Builder
	cursor := 0
	for _, tok := range tokens {
		if tok.Start > cursor {
			b.WriteString(input[cursor:tok.Start])
		}
		if tok.End > cursor {
			cursor = tok.End
		}
	}
	if cursor < len(input) {
		b.WriteString(input[cursor:])
	}
	name := strings.Join(strings.Fields(b.String()), " ")

	recur := ""
	if recurFound {
		recur = recurRule.String()
	}

	return ParsedTask{
		Name:       name,
		Category:   category,
		Priority:   priority,
		Status:     status,
		DueAt:      dueAt,
		DueHasTime: dueHasTime,
		Recur:      recur,
		Tokens:     tokens,
	}
}

// FormatTask renders a task back into parser input syntax, omitting
// whatever is unset. Used to pre-fill the edit box; ParseTask(FormatTask(t),
// now) round-trips Category, Priority, Status, DueAt and DueHasTime.
func FormatTask(t Task) string {
	var b strings.Builder
	b.WriteString(t.Name)

	if t.CategoryName != "" {
		b.WriteString(" #" + t.CategoryName)
	}

	switch t.Priority {
	case PriorityLow:
		b.WriteString(" !low")
	case PriorityMed:
		b.WriteString(" !med")
	case PriorityHigh:
		b.WriteString(" !high")
	}

	// Status always has a concrete value on a stored task (it defaults to
	// "todo"), so it is always emitted - round-tripping FormatTask's output
	// back through ParseTask must reproduce it, and an omitted @status
	// would parse back as "" ("leave default"), not "todo".
	status := t.Status
	if status == "" {
		status = "todo"
	}
	b.WriteString(" @" + status)

	if t.RecurRule != "" {
		b.WriteString(" " + t.RecurRule)
	}

	if t.DueAt.Valid {
		d := t.DueAt.Time
		b.WriteString(" " + d.Format("2006-01-02"))
		if t.DueHasTime {
			b.WriteString(" " + d.Format("15:04"))
		}
	}

	return strings.TrimSpace(b.String())
}
