package reserver

import (
	"testing"
)

// rearm must forget the last seen seat count ONLY for slots whose demand
// rose, so a recycled (or new) booking on a slot with unchanged seats is
// re-attempted on the next poll instead of waiting for a seat change.
func TestRearmForgetsSeatCountOnlyWhereDemandRose(t *testing.T) {
	prev := Count{"WToJ": {"03-07-2026": {"08:00": 2, "13:45": 1, "19:00": 4}}}
	old := Count{"WToJ": {"03-07-2026": {"08:00:00": 1, "13:45:00": 1, "19:00:00": 2}}}
	next := Count{"WToJ": {"03-07-2026": {"08:00:00": 2, "13:45:00": 1, "19:00:00": 1}}}

	rearmed := rearm(prev, old, next)

	if _, ok := prev["WToJ"]["03-07-2026"]["08:00"]; ok {
		t.Error("08:00 demand rose 1 -> 2: its seat count must be forgotten")
	}
	if prev["WToJ"]["03-07-2026"]["13:45"] != 1 {
		t.Error("13:45 demand unchanged: its seat count must be kept")
	}
	if prev["WToJ"]["03-07-2026"]["19:00"] != 4 {
		t.Error("19:00 demand fell: its seat count must be kept")
	}
	if len(rearmed) != 1 || rearmed[0] != "WToJ:03-07-2026:08:00:00" {
		t.Errorf("unexpected re-armed slots %v", rearmed)
	}
}

// a slot that newly gains demand (absent from old) is re-armed; one never
// seen by the poller (absent from prev) is a harmless no-op
func TestRearmNewDemandAndUnseenSlots(t *testing.T) {
	prev := Count{"JToW": {"04-07-2026": {"10:00": 3}}}
	old := Count{}
	next := Count{"JToW": {"04-07-2026": {"10:00:00": 1, "11:00:00": 1}}, "WToJ": {"05-07-2026": {"07:00:00": 1}}}

	rearmed := rearm(prev, old, next)

	if _, ok := prev["JToW"]["04-07-2026"]["10:00"]; ok {
		t.Error("10:00 gained demand: its seat count must be forgotten")
	}
	if len(rearmed) != 3 {
		t.Errorf("expected 3 re-armed slots, got %v", rearmed)
	}
}

func TestCopyCountIsDeep(t *testing.T) {
	c := Count{"WToJ": {"03-07-2026": {"08:00:00": 1}}}
	cp := copyCount(c)
	c["WToJ"]["03-07-2026"]["08:00:00"] = 5
	if cp["WToJ"]["03-07-2026"]["08:00:00"] != 1 {
		t.Error("copyCount must not share nested maps")
	}
}
