package prober

import (
	"reflect"
	"testing"
)

func TestTargetsFromCountKeepsEveryDemandedSlotSorted(t *testing.T) {
	counts := map[string]map[string]map[string]int{
		"WToJ": {"02-01-2027": {"09:00:00": 2}},
		"JToW": {
			"02-01-2027": {"08:30:00": 1},
			"01-01-2027": {"10:00:00": 3, "11:00:00": 0},
		},
	}
	want := []Target{
		{Direction: "JToW", Date: "01-01-2027", Time: "10:00:00", Needed: 3},
		{Direction: "JToW", Date: "02-01-2027", Time: "08:30:00", Needed: 1},
		{Direction: "WToJ", Date: "02-01-2027", Time: "09:00:00", Needed: 2},
	}
	if got := TargetsFromCount(counts); !reflect.DeepEqual(got, want) {
		t.Fatalf("TargetsFromCount() = %#v, want %#v", got, want)
	}
}

func TestShardTargetsCoversAllSlots(t *testing.T) {
	targets := []Target{{Time: "1"}, {Time: "2"}, {Time: "3"}, {Time: "4"}, {Time: "5"}}
	shards := ShardTargets(targets, 2)
	if len(shards) != 3 || len(shards[0]) != 2 || len(shards[1]) != 2 || len(shards[2]) != 1 {
		t.Fatalf("unexpected shards: %#v", shards)
	}
	var flattened []Target
	for _, shard := range shards {
		flattened = append(flattened, shard...)
	}
	if !reflect.DeepEqual(flattened, targets) {
		t.Fatalf("sharding lost or reordered targets: %#v", flattened)
	}
}

func TestMatchesIsCaseInsensitiveAndIgnoresEmptyPatterns(t *testing.T) {
	if !Matches([]string{"There are NO SEATS available."}, []string{"", "no seats"}) {
		t.Fatal("expected sold-out message to match")
	}
	if Matches([]string{"session expired"}, nil) {
		t.Fatal("empty patterns must not match")
	}
}

func TestParseTargetsRejectsUnsafeInput(t *testing.T) {
	if _, err := ParseTargets(`[{"dir":"JToW","date":"01-01-2027","time":"08:30:00","needed":0}]`); err == nil {
		t.Fatal("expected zero hold budget to be rejected")
	}
	if _, err := ParseTargets(`not-json`); err == nil {
		t.Fatal("expected invalid JSON to be rejected")
	}
}

func TestSumTallies(t *testing.T) {
	tally := SumTallies(42, "job", []SlotTally{{Polls: 3, Holds: 1, SoldOut: 2}, {Polls: 4, Errors: 1}})
	if tally.Total.Polls != 7 || tally.Total.Holds != 1 || tally.Total.SoldOut != 2 || tally.Total.Errors != 1 {
		t.Fatalf("unexpected total: %#v", tally.Total)
	}
}
