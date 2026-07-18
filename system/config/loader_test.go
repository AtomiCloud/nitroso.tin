package config

import (
	"reflect"
	"testing"
)

func TestLoaderReadsProberDefaults(t *testing.T) {
	loader := Loader{Landscape: "pichu", BaseConfig: "../../config/app"}
	cfg, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := ProberConfig{
		EpochMinutes: 1, JobMinutes: 2, JobCpu: "250m", JobMemory: "128Mi",
		SlotsPerJob: 500, Fanout: 1,
		PaceMs: 0, DryRun: true, ErrorLimit: 5, ErrorBackoffMs: 100,
		ReleaseDrainLimit: 10, ReleaseDrainBudgetMs: 5000,
		ReleaseTerminalPatterns: []string{"not found", "booking expired", "invalid booking"},
		SoldOutPatterns:         []string{"sold out", "no seat", "not available", "not enough seat"},
		StaleDataPatterns:       []string{"search data", "trip data", "expired"},
		SessionPatterns:         []string{"session", "login", "unauthorized"},
		RateLimitPatterns:       []string{"too many requests", "rate limit"},
	}
	if !reflect.DeepEqual(cfg.Prober, want) {
		t.Fatalf("unexpected prober defaults:\n got: %#v\nwant: %#v", cfg.Prober, want)
	}
}
