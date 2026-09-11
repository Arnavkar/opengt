package main

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestMeasureRecordsSpan(t *testing.T) {
	var tm Timer
	tm.Enabled = true

	err := tm.Measure("step", func() error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tm.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(tm.Spans))
	}
	if tm.Spans[0].Name != "step" {
		t.Errorf("name = %q, want %q", tm.Spans[0].Name, "step")
	}
	if tm.Spans[0].Duration <= 0 {
		t.Errorf("duration not recorded: %v", tm.Spans[0].Duration)
	}
}

// Spans are flat in completion order: the inner Measure finishes first, so it
// is appended before the outer one. This documents the recorded shape.
func TestNestedMeasureRecordsInOrder(t *testing.T) {
	var tm Timer
	tm.Enabled = true

	if err := tm.Measure("outer", func() error {
		return tm.Measure("inner", func() error { return nil })
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tm.Spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(tm.Spans))
	}
	if tm.Spans[0].Name != "inner" || tm.Spans[1].Name != "outer" {
		t.Errorf("order = [%s, %s], want [inner, outer]", tm.Spans[0].Name, tm.Spans[1].Name)
	}
}

func TestMeasureErrorPathRecords(t *testing.T) {
	var tm Timer
	tm.Enabled = true
	sent := errors.New("boom")

	err := tm.Measure("step", func() error { return sent })
	if !errors.Is(err, sent) {
		t.Errorf("error = %v, want %v", err, sent)
	}
	if len(tm.Spans) != 1 {
		t.Fatalf("expected span recorded on error, got %d", len(tm.Spans))
	}
	if tm.Spans[0].Name != "step" {
		t.Errorf("name = %q, want %q", tm.Spans[0].Name, "step")
	}
}

func TestStartRecords(t *testing.T) {
	var tm Timer
	tm.Enabled = true

	stop := tm.Start("step")
	time.Sleep(time.Millisecond)
	stop()
	if len(tm.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(tm.Spans))
	}
	if tm.Spans[0].Name != "step" {
		t.Errorf("name = %q, want %q", tm.Spans[0].Name, "step")
	}
	if tm.Spans[0].Duration <= 0 {
		t.Errorf("duration not recorded: %v", tm.Spans[0].Duration)
	}
}

func TestDisabledMeasureRunsFnButRecordsNoSpan(t *testing.T) {
	var tm Timer
	ran := false

	err := tm.Measure("step", func() error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Error("fn did not run when disabled")
	}
	if len(tm.Spans) != 0 {
		t.Errorf("expected no spans when disabled, got %d", len(tm.Spans))
	}
}

func TestDisabledStartIsNoOp(t *testing.T) {
	var tm Timer
	stop := tm.Start("step")
	stop()
	if len(tm.Spans) != 0 {
		t.Errorf("expected no spans when disabled, got %d", len(tm.Spans))
	}
}

// Test matrix #14: timing must not contaminate stdout. timing.go never prints,
// but assert it directly so a future change can't silently regress.
func TestTimingDoesNotContaminateStdout(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	var tm Timer
	tm.Enabled = true
	_ = tm.Measure("step", func() error { return nil })
	stop := tm.Start("step2")
	stop()

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	out, _ := io.ReadAll(r)
	if len(out) != 0 {
		t.Errorf("stdout contaminated: %q", out)
	}
}
