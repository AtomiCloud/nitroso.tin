package prober

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"runtime"
	"runtime/pprof"
	"sync"
)

const (
	profileCPUPrefix       = "PPROF-CPU-B64:"
	profileHeapPrefix      = "PPROF-HEAP-B64:"
	profileGoroutinePrefix = "PPROF-GOROUTINE-B64:"
	profileBlockPrefix     = "PPROF-BLOCK-B64:"
	profileMutexPrefix     = "PPROF-MUTEX-B64:"
)

type capturedProfile struct {
	name   string
	prefix string
	data   []byte
}

// Profiler captures a CPU profile for its full lifetime and emits terminal
// runtime profiles when stopped. A nil *Profiler is the disabled session.
type Profiler struct {
	output                       io.Writer
	cpu                          bytes.Buffer
	previousMutexProfileFraction int
	stopOnce                     sync.Once
	stopErr                      error
}

// StartProfiler returns nil without allocating buffers or enabling runtime
// sampling when enabled is false.
func StartProfiler(enabled bool, output io.Writer) (*Profiler, error) {
	if !enabled {
		return nil, nil
	}
	if output == nil {
		return nil, errors.New("profile output is required")
	}

	profiler := &Profiler{output: output}
	runtime.SetBlockProfileRate(1)
	profiler.previousMutexProfileFraction = runtime.SetMutexProfileFraction(1)
	if err := pprof.StartCPUProfile(&profiler.cpu); err != nil {
		runtime.SetBlockProfileRate(0)
		runtime.SetMutexProfileFraction(profiler.previousMutexProfileFraction)
		return nil, fmt.Errorf("start CPU profile: %w", err)
	}
	return profiler, nil
}

// Stop ends CPU profiling, captures the terminal profiles, disables sampling,
// and writes one gzip+base64 log line per profile. It is safe to call twice.
func (p *Profiler) Stop() error {
	if p == nil {
		return nil
	}
	p.stopOnce.Do(func() {
		p.stopErr = p.stop()
	})
	return p.stopErr
}

func (p *Profiler) stop() error {
	pprof.StopCPUProfile()

	var stopErr error
	profiles := []capturedProfile{{name: "cpu", prefix: profileCPUPrefix, data: p.cpu.Bytes()}}
	runtime.GC()
	for _, spec := range []struct {
		name   string
		prefix string
	}{
		{name: "heap", prefix: profileHeapPrefix},
		{name: "goroutine", prefix: profileGoroutinePrefix},
		{name: "block", prefix: profileBlockPrefix},
		{name: "mutex", prefix: profileMutexPrefix},
	} {
		profile := pprof.Lookup(spec.name)
		if profile == nil {
			stopErr = errors.Join(stopErr, fmt.Errorf("runtime profile %q is unavailable", spec.name))
			continue
		}
		var data bytes.Buffer
		if err := profile.WriteTo(&data, 0); err != nil {
			stopErr = errors.Join(stopErr, fmt.Errorf("capture %s profile: %w", spec.name, err))
			continue
		}
		profiles = append(profiles, capturedProfile{name: spec.name, prefix: spec.prefix, data: data.Bytes()})
	}

	runtime.SetBlockProfileRate(0)
	runtime.SetMutexProfileFraction(p.previousMutexProfileFraction)

	for _, profile := range profiles {
		if err := writeProfileLine(p.output, profile.prefix, profile.data); err != nil {
			stopErr = errors.Join(stopErr, fmt.Errorf("emit %s profile: %w", profile.name, err))
		}
	}
	return stopErr
}

func writeProfileLine(output io.Writer, prefix string, profile []byte) error {
	var compressed bytes.Buffer
	zipper := gzip.NewWriter(&compressed)
	if _, err := zipper.Write(profile); err != nil {
		return err
	}
	if err := zipper.Close(); err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(compressed.Bytes())
	_, err := fmt.Fprintf(output, "%s%s\n", prefix, encoded)
	return err
}
