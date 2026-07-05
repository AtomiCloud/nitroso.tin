package buyer

import (
	"fmt"
	"strings"
)

// ConflictError marks a KTMB rejection that means this passenger already holds
// a ticket for the slot. KTMB's SetPassenger wording is "Duplicated passport
// number for onward trip : <passport>." (matched via conflictPatterns). The
// booking should be parked for recovery instead of crash-looping the buyer.
type ConflictError struct {
	Messages []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("ktmb conflict (duplicate passenger): %+v", e.Messages)
}

// RevertError marks a KTMB buy that failed for a transient reason that captured
// NO ticket (e.g. "wallet balance is insufficient" at Pay). The booking should
// be reverted Buying -> Pending to retry once the condition clears, rather than
// stranded in Buying.
type RevertError struct {
	Messages []string
}

func (e *RevertError) Error() string {
	return fmt.Sprintf("ktmb transient failure (revert to pending): %+v", e.Messages)
}

// PurchasedError marks a purchase that SUCCEEDED on KTMB (payment captured,
// booking confirmed) but whose ticket artifacts could not be retrieved. The
// ticket exists — callers must never treat this as a failed buy or release
// the reservation; recover deterministically via BookingNo/TicketNo.
type PurchasedError struct {
	BookingNo string
	TicketNo  string
	Cause     error
}

func (e *PurchasedError) Error() string {
	return fmt.Sprintf("ktmb purchase succeeded (booking %s, ticket %s) but ticket retrieval failed: %v", e.BookingNo, e.TicketNo, e.Cause)
}

func (e *PurchasedError) Unwrap() error {
	return e.Cause
}

func matchesConflict(patterns, messages []string) bool {
	for _, m := range messages {
		lm := strings.ToLower(m)
		for _, p := range patterns {
			if p != "" && strings.Contains(lm, strings.ToLower(p)) {
				return true
			}
		}
	}
	return false
}
