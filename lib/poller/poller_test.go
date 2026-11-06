package poller

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func newTestPoller(maxStreams int, tiers Tiers) *Poller {
	l := zerolog.Nop()
	return &Poller{logger: &l, maxStreams: maxStreams, tiers: tiers}
}

func targets(dates ...string) []HeliumJob {
	out := make([]HeliumJob, 0, len(dates)*2)
	for _, d := range dates {
		out = append(out, HeliumJob{Date: d, From: "WToJ"}, HeliumJob{Date: d, From: "JToW"})
	}
	return out
}

func dates(js []HeliumJob) []string {
	out := make([]string, len(js))
	for i, j := range js {
		out[i] = j.Date
	}
	return out
}

func TestWindowIsFullPowerRestIsLight(t *testing.T) {
	p := newTestPoller(4, Tiers{MaxBoost: 10, MaxLight: 100})
	full, light := p.selectTargets(context.Background(), targets("10-11-2026", "09-11-2026", "05-01-2027"), nil, time.Now())
	if len(full) != 4 || full[0].Date != "09-11-2026" || full[3].Date != "10-11-2026" {
		t.Fatalf("full should be the earliest 4 targets, got %v", dates(full))
	}
	if len(light) != 2 || light[0].Date != "05-01-2027" {
		t.Fatalf("later dates should be light, got %v", dates(light))
	}
}

func TestReleasedDatesGetFullPowerUntilExpiry(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	p := newTestPoller(2, Tiers{MaxBoost: 10, MaxLight: 100})
	boost := map[string]int64{"05-01-2027": now.Unix() + 3600, "06-01-2027": now.Unix() - 1}
	full, light := p.selectTargets(context.Background(), targets("09-11-2026", "05-01-2027", "06-01-2027", "07-01-2027"), boost, now)
	if got := dates(full); len(got) != 4 || got[2] != "05-01-2027" || got[3] != "05-01-2027" {
		t.Fatalf("window + unexpired released date at full power, got %v", got)
	}
	if got := dates(light); len(got) != 4 || got[0] != "06-01-2027" {
		t.Fatalf("expired + unreleased dates fall to light, got %v", got)
	}
}

func TestBoostCapOverflowsToLight(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	exp := now.Unix() + 60
	p := newTestPoller(2, Tiers{MaxBoost: 3, MaxLight: 100})
	boost := map[string]int64{"05-01-2027": exp, "06-01-2027": exp, "07-01-2027": exp}
	full, light := p.selectTargets(context.Background(), targets("09-11-2026", "05-01-2027", "06-01-2027", "07-01-2027"), boost, now)
	if len(full) != 5 || full[4].Date != "06-01-2027" {
		t.Fatalf("window(2) + earliest 3 released, got %v", dates(full))
	}
	if len(light) != 3 {
		t.Fatalf("boost overflow goes light, got %v", dates(light))
	}
}

func TestLightDisabledAndCapped(t *testing.T) {
	p := newTestPoller(2, Tiers{MaxLight: 0})
	full, light := p.selectTargets(context.Background(), targets("09-11-2026", "05-01-2027"), nil, time.Now())
	if len(full) != 2 || len(light) != 0 {
		t.Fatalf("MaxLight 0 disables light, got full=%v light=%v", dates(full), dates(light))
	}
	p = newTestPoller(2, Tiers{MaxLight: 3})
	_, light = p.selectTargets(context.Background(), targets("09-11-2026", "05-01-2027", "06-01-2027"), nil, time.Now())
	if len(light) != 3 || light[0].Date != "05-01-2027" {
		t.Fatalf("light capped at 3 earliest, got %v", dates(light))
	}
}

func TestTargetSignatureChangesWithTargets(t *testing.T) {
	a := targetSignature(targets("09-11-2026"), targets("05-01-2027"))
	b := targetSignature(targets("09-11-2026"), targets("05-01-2027"))
	c := targetSignature(targets("09-11-2026", "10-11-2026"), targets("05-01-2027"))
	d := targetSignature(targets("09-11-2026", "05-01-2027"), nil)
	if a != b {
		t.Fatal("same targets must give the same signature")
	}
	if a == c || a == d {
		t.Fatal("a new date, or a date moving level, must change the signature")
	}
}

func TestClaimGenerationFailsOpenWithoutRedis(t *testing.T) {
	p := newTestPoller(2, Tiers{GenerationKey: "k", MinGeneration: time.Minute})
	if !p.claimGeneration(context.Background(), targets("09-11-2026"), nil) {
		t.Fatal("no redis must spawn as before")
	}
}
