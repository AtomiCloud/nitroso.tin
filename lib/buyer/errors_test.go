package buyer

import "testing"

func TestMatchesConflict(t *testing.T) {
	patterns := []string{"duplicate passport"}

	cases := []struct {
		name     string
		messages []string
		want     bool
	}{
		{"duplicate passport message", []string{"Duplicate passport no. found in the same trip."}, true},
		{"case insensitive", []string{"DUPLICATE PASSPORT NO."}, true},
		{"unrelated error", []string{"Session expired"}, false},
		{"unrelated duplicate error does not match tightened pattern", []string{"duplicate request submitted"}, false},
		{"empty messages", []string{}, false},
		{"one of many matches", []string{"Something else", "duplicate passport found"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesConflict(patterns, tc.messages); got != tc.want {
				t.Errorf("matchesConflict(%v) = %v, want %v", tc.messages, got, tc.want)
			}
		})
	}
}

func TestMatchesConflictEmptyPatterns(t *testing.T) {
	if matchesConflict(nil, []string{"duplicate passport"}) {
		t.Error("nil patterns must never match")
	}
	if matchesConflict([]string{""}, []string{"anything"}) {
		t.Error("empty pattern must never match")
	}
}
