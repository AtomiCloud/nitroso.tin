package config

import "testing"

func TestLoaderReadsProberDefaults(t *testing.T) {
	loader := Loader{Landscape: "pichu", BaseConfig: "../../config/app"}
	cfg, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Prober.EpochMinutes != 1 || cfg.Prober.JobMinutes != 2 || cfg.Prober.SlotsPerJob != 500 || cfg.Prober.Fanout != 1 {
		t.Fatalf("unexpected prober fleet defaults: %#v", cfg.Prober)
	}
	if !cfg.Prober.DryRun {
		t.Fatal("prober must default to dry-run")
	}
	if len(cfg.Prober.SoldOutPatterns) == 0 || len(cfg.Prober.SessionPatterns) == 0 {
		t.Fatal("prober response patterns were not loaded")
	}
}
