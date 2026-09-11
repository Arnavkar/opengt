package main

import "time"

// Measure runs fn and records its elapsed time as a span when t.Enabled.
// The span is recorded even when fn errors; fn's error is returned unchanged.
// Spans are appended flat in completion order, so an inner Measure that runs
// inside an outer one lands in t.Spans before the outer one (inner finishes
// first). The Sub field is left for a renderer to reshape if a tree is wanted;
// Measure itself does not populate it.
func (t *Timer) Measure(name string, fn func() error) error {
	if !t.Enabled {
		return fn()
	}
	start := time.Now()
	err := fn()
	t.Spans = append(t.Spans, TimingSpan{Name: name, Duration: time.Since(start)})
	return err
}

// Start returns a stop func that records the elapsed span when called.
// When t.Enabled is false, both Start and the returned func are no-ops so
// nothing is appended and stdout stays clean.
func (t *Timer) Start(name string) func() {
	if !t.Enabled {
		return func() {}
	}
	start := time.Now()
	return func() {
		t.Spans = append(t.Spans, TimingSpan{Name: name, Duration: time.Since(start)})
	}
}
