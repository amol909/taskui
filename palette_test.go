package main

import (
	"strings"
	"testing"
)

// TestFuzzyMatch_EmptyNeedleMatchesEverything proves an empty needle matches
// every haystack with score 0 - this is what lets the palette show every
// command before the user has typed anything.
func TestFuzzyMatch_EmptyNeedleMatchesEverything(t *testing.T) {
	for _, haystack := range []string{"", "Set status: blocked", "x"} {
		score, ok := fuzzyMatch("", haystack)
		if !ok {
			t.Errorf("fuzzyMatch(%q, %q) ok = false, want true", "", haystack)
		}
		if score != 0 {
			t.Errorf("fuzzyMatch(%q, %q) score = %d, want 0", "", haystack, score)
		}
	}
}

// TestFuzzyMatch_NoMatch proves a needle whose characters don't appear as a
// subsequence of haystack fails outright.
func TestFuzzyMatch_NoMatch(t *testing.T) {
	tests := []struct{ needle, haystack string }{
		{"xyz", "Set status: blocked"},
		{"zzz", "Toggle project scope"},
		{"blocked!", "Set status: blocked"}, // trailing char not present
	}
	for _, tt := range tests {
		if _, ok := fuzzyMatch(tt.needle, tt.haystack); ok {
			t.Errorf("fuzzyMatch(%q, %q) ok = true, want false", tt.needle, tt.haystack)
		}
	}
}

// TestFuzzyMatch_CaseInsensitiveSubsequence proves matching is case
// insensitive and accepts a scattered (non-contiguous) subsequence.
func TestFuzzyMatch_CaseInsensitiveSubsequence(t *testing.T) {
	if _, ok := fuzzyMatch("BLO", "Set status: blocked"); !ok {
		t.Errorf("expected case-insensitive match to succeed")
	}
	if _, ok := fuzzyMatch("sttbl", "Set status: blocked"); !ok {
		t.Errorf("expected a scattered subsequence match to succeed")
	}
}

// TestFuzzyMatch_RankingBloRanksSetStatusBlockedAboveToggleProjectScope is
// the ranking assertion called out by the plan: "blo" must rank
// "Set status: blocked" above "Toggle project scope" - "blo" starts a word
// in the former (a run right after a boundary) and is scattered across two
// separate words in the latter.
func TestFuzzyMatch_RankingBloRanksSetStatusBlockedAboveToggleProjectScope(t *testing.T) {
	scoreBlocked, ok := fuzzyMatch("blo", "Set status: blocked")
	if !ok {
		t.Fatalf("expected fuzzyMatch(blo, Set status: blocked) to match")
	}
	// NB: the original spec for this test named "Toggle project scope" as the
	// weaker match, but that string contains no 'b' at all, so "blo" can never
	// match it. Use a haystack that genuinely contains b-l-o as a scattered
	// subsequence, which is what the ranking rule is actually about.
	scoreScattered, ok := fuzzyMatch("blo", "Browse all options")
	if !ok {
		t.Fatalf("expected fuzzyMatch(blo, Browse all options) to match")
	}
	if scoreBlocked <= scoreScattered {
		t.Errorf("score(%q against %q) = %d, want > score(%q against %q) = %d",
			"blo", "Set status: blocked", scoreBlocked, "blo", "Browse all options", scoreScattered)
	}
}

// TestFuzzyMatch_ExactPrefixOutranksScatteredSubsequence proves a needle
// that is an exact, contiguous, word-boundary prefix of one haystack outranks
// the same needle matched as a scattered subsequence of another.
func TestFuzzyMatch_ExactPrefixOutranksScatteredSubsequence(t *testing.T) {
	prefixScore, ok := fuzzyMatch("set", "Set status: blocked")
	if !ok {
		t.Fatalf("expected prefix match to succeed")
	}
	scatteredScore, ok := fuzzyMatch("set", "Switch project scope though")
	if !ok {
		t.Fatalf("expected scattered match to succeed")
	}
	if prefixScore <= scatteredScore {
		t.Errorf("exact prefix score %d should outrank scattered subsequence score %d", prefixScore, scatteredScore)
	}
}

// TestFuzzyMatch_ConsecutiveRunScoresHigherThanGaps proves a needle matched
// as one consecutive run scores higher than the same needle scattered with
// gaps between each character.
func TestFuzzyMatch_ConsecutiveRunScoresHigherThanGaps(t *testing.T) {
	consecutive, ok := fuzzyMatch("cat", "vacation")
	if !ok {
		t.Fatalf("expected consecutive match to succeed")
	}
	gappy, ok := fuzzyMatch("cat", "cake at table")
	if !ok {
		t.Fatalf("expected gappy match to succeed")
	}
	if consecutive <= gappy {
		t.Errorf("consecutive run score %d should outrank gappy score %d", consecutive, gappy)
	}
}

// TestFuzzyMatch_WordBoundaryBonus proves a match starting right after a
// space/-/: boundary scores higher than the identical characters matched
// mid-word.
func TestFuzzyMatch_WordBoundaryBonus(t *testing.T) {
	boundary, ok := fuzzyMatch("pro", "Switch project")
	if !ok {
		t.Fatalf("expected boundary match to succeed")
	}
	midWord, ok := fuzzyMatch("pro", "Approach")
	if !ok {
		t.Fatalf("expected mid-word match to succeed")
	}
	if boundary <= midWord {
		t.Errorf("word-boundary match score %d should outrank mid-word match score %d", boundary, midWord)
	}
}

// TestCommandsFor_OmitsTaskCommandsWhenNothingSelected proves task-specific
// commands (set status, set priority, delete, toggle complete, ...) only
// appear when a task is selected under the cursor.
func TestCommandsFor_OmitsTaskCommandsWhenNothingSelected(t *testing.T) {
	m := &model{
		view:   viewList,
		tasks:  nil,
		cursor: -1,
		keyMap: DefaultKeyMap,
	}

	// "Task" is a display group, not a precondition: "Add task" and "Undo
	// delete" belong to it but operate without a selection — and Add task is
	// exactly what the palette should offer when the list is empty. Assert on
	// the commands that actually act on the task under the cursor.
	needsSelection := map[string]bool{
		"cycle-status":    true,
		"delete-task":     true,
		"toggle-complete": true,
	}

	cmds := commandsFor(m)
	for _, c := range cmds {
		if needsSelection[c.ID] || strings.HasPrefix(c.ID, "set-status-") || strings.HasPrefix(c.ID, "set-priority-") {
			t.Errorf("expected no selection-dependent commands when nothing is selected, found %q", c.Title)
		}
	}
}

// TestCommandsFor_IncludesTaskCommandsWhenSelected proves the converse: with
// a task under the cursor, task-specific commands are present.
func TestCommandsFor_IncludesTaskCommandsWhenSelected(t *testing.T) {
	m := &model{
		view:   viewList,
		tasks:  []Task{{ID: 1, Name: "write the report", Status: "todo"}},
		cursor: 0,
		keyMap: DefaultKeyMap,
	}

	cmds := commandsFor(m)
	found := false
	for _, c := range cmds {
		if c.Group == "Task" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected at least one Task-group command when a task is selected")
	}
}

// TestCommandsFor_SaveCurrentViewAlwaysOffered proves "Save current view…"
// is offered regardless of model state (no saved views, no active view,
// nothing selected) - it is not gated behind any precondition the way
// task-specific commands are.
func TestCommandsFor_SaveCurrentViewAlwaysOffered(t *testing.T) {
	m := &model{
		view:   viewList,
		cursor: -1,
		keyMap: DefaultKeyMap,
	}

	cmds := commandsFor(m)
	for _, c := range cmds {
		if c.ID == "save-view" && c.Title == "Save current view…" && c.Group == "View" {
			return
		}
	}
	t.Errorf("expected a %q command, got %+v", "Save current view…", cmds)
}

// TestCommandsFor_DeleteViewOnlyForUserViews proves "Delete view: X" is
// offered per user-defined saved view and never for a built-in one - the
// palette is the only deletion UI, and built-ins must never be deletable
// from it (the "1"-"4" keybindings assume they always exist).
func TestCommandsFor_DeleteViewOnlyForUserViews(t *testing.T) {
	userView := SavedView{ID: 101, Name: "My Custom View", Position: 5, BuiltIn: false}
	m := &model{
		view:       viewList,
		cursor:     -1,
		keyMap:     DefaultKeyMap,
		savedViews: append(append([]SavedView{}, builtInViews...), userView),
	}

	cmds := commandsFor(m)

	var deleteTitles []string
	for _, c := range cmds {
		if strings.HasPrefix(c.ID, "delete-view-") {
			deleteTitles = append(deleteTitles, c.Title)
		}
	}

	if len(deleteTitles) != 1 || deleteTitles[0] != "Delete view: My Custom View" {
		t.Errorf("expected exactly one delete-view command for the user view, got %v", deleteTitles)
	}

	for _, v := range builtInViews {
		wantAbsent := "Delete view: " + v.Name
		for _, title := range deleteTitles {
			if title == wantAbsent {
				t.Errorf("expected no delete command for built-in view %q, found %q", v.Name, title)
			}
		}
	}
}
