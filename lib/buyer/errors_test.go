package buyer

import "testing"

func TestMatchesConflict(t *testing.T) {
	// mirror the deployed config (config/app + infra/consumer_chart settings.yaml)
	patterns := []string{"duplicated passport", "duplicate passport"}

	cases := []struct {
		name     string
		messages []string
		want     bool
	}{
		// the real KTMB SetPassenger rejection — the string this fix exists for
		{"real ktmb onward-trip message", []string{"Duplicated passport number for onward trip : K4461909G."}, true},
		{"real ktmb return-trip variant", []string{"Duplicated passport number for return trip : A1234567."}, true},
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

// regression guard: the OLD pattern alone must NOT match KTMB's real wording —
// this is the exact bug (extra 'd' in "Duplicated") that stranded conflicts in
// Buying. If someone drops 'duplicated passport', this test fails loudly.
func TestOldPatternMissesRealKtmbMessage(t *testing.T) {
	real := []string{"Duplicated passport number for onward trip : K4461909G."}
	if matchesConflict([]string{"duplicate passport"}, real) {
		t.Fatal("'duplicate passport' unexpectedly matched the real KTMB message; the 'duplicated passport' pattern would be redundant")
	}
	if !matchesConflict([]string{"duplicated passport"}, real) {
		t.Fatal("'duplicated passport' must match the real KTMB message")
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

// The buyer reverts a booking Buying -> Pending when Pay fails with a transient,
// ticket-less error. KTMB's real message is "KTM Wallet balance is insufficient.";
// the configured revertPatterns must match it (case-insensitive substring) but
// NOT an unrelated failure like a duplicate-passport conflict.
func TestRevertPatternMatchesWalletInsufficient(t *testing.T) {
	patterns := []string{"wallet balance is insufficient"}
	if !matchesConflict(patterns, []string{"KTM Wallet balance is insufficient."}) {
		t.Fatal("revert pattern must match KTMB's wallet-insufficient message")
	}
	if matchesConflict(patterns, []string{"Duplicated passport number for onward trip : K4461909G."}) {
		t.Fatal("revert pattern must not match a duplicate-passport error")
	}
	if matchesConflict(patterns, []string{"Not enough seat for onward trip."}) {
		t.Fatal("revert pattern must not match an out-of-seats error")
	}
}
