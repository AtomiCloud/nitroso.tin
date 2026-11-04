package prober

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"strings"
	"testing"
	"time"
)

func TestProfilerDisabledIsInert(t *testing.T) {
	profiler, err := StartProfiler(false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if profiler != nil {
		t.Fatalf("disabled profiler = %#v, want nil", profiler)
	}
}

func TestProfilerEnabledWritesDecodableProfiles(t *testing.T) {
	var output bytes.Buffer
	profiler, err := StartProfiler(true, &output)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = profiler.Stop() })

	// Let the 100 Hz CPU sampler observe the enabled session.
	time.Sleep(120 * time.Millisecond)
	if err := profiler.Stop(); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	prefixes := []string{
		profileCPUPrefix,
		profileHeapPrefix,
		profileGoroutinePrefix,
		profileBlockPrefix,
		profileMutexPrefix,
	}
	if len(lines) != len(prefixes) {
		t.Fatalf("profile line count = %d, want %d", len(lines), len(prefixes))
	}
	for i, prefix := range prefixes {
		if !strings.HasPrefix(lines[i], prefix) {
			t.Fatalf("profile line %d = %q, want prefix %q", i, lines[i], prefix)
		}
		encoded := strings.TrimPrefix(lines[i], prefix)
		compressed, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatalf("decode %s: %v", prefix, err)
		}
		zipper, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			t.Fatalf("open %s gzip payload: %v", prefix, err)
		}
		profile, err := io.ReadAll(zipper)
		if err != nil {
			t.Fatalf("read %s gzip payload: %v", prefix, err)
		}
		if err := zipper.Close(); err != nil {
			t.Fatalf("close %s gzip payload: %v", prefix, err)
		}
		if len(profile) == 0 {
			t.Fatalf("%s profile is empty", prefix)
		}
		if !bytes.HasPrefix(profile, []byte{0x1f, 0x8b}) {
			t.Fatalf("%s payload is not a Go gzip-compressed pprof profile", prefix)
		}
	}

	before := output.Len()
	if err := profiler.Stop(); err != nil {
		t.Fatal(err)
	}
	if output.Len() != before {
		t.Fatal("second Stop wrote duplicate profiles")
	}
}
