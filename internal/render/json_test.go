package render

import (
	"testing"
	"time"
)

func TestJSONRendererCollectsEndEvents(t *testing.T) {
	r := &JSONRenderer{}

	r.HandleEvent(Event{
		Type:   EventTaskEnd,
		TS:     time.Now(),
		TaskID: "task-1",
		Data:   map[string]any{"status": "ok", "message": "synced"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskEnd,
		TS:     time.Now(),
		TaskID: "task-2",
		Data:   map[string]any{"status": "error", "message": "conflict"},
	})

	results := r.Results()
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].TaskID != "task-1" {
		t.Errorf("results[0].TaskID: got %q, want %q", results[0].TaskID, "task-1")
	}
	if results[0].Status != "ok" {
		t.Errorf("results[0].Status: got %q, want %q", results[0].Status, "ok")
	}
	if results[0].Message != "synced" {
		t.Errorf("results[0].Message: got %q, want %q", results[0].Message, "synced")
	}

	if results[1].TaskID != "task-2" {
		t.Errorf("results[1].TaskID: got %q, want %q", results[1].TaskID, "task-2")
	}
	if results[1].Status != "error" {
		t.Errorf("results[1].Status: got %q, want %q", results[1].Status, "error")
	}
	if results[1].Message != "conflict" {
		t.Errorf("results[1].Message: got %q, want %q", results[1].Message, "conflict")
	}
}

func TestJSONRendererIgnoresNonEndEvents(t *testing.T) {
	r := &JSONRenderer{}

	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: "task-1",
		Data:   map[string]any{"name": "sync"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskLog,
		TS:     time.Now(),
		TaskID: "task-1",
		Data:   map[string]any{"text": "rebasing"},
	})
	r.HandleEvent(Event{
		Type:   EventPause,
		TS:     time.Now(),
		TaskID: "task-1",
	})
	r.HandleEvent(Event{
		Type:   EventResume,
		TS:     time.Now(),
		TaskID: "task-1",
	})

	results := r.Results()
	if len(results) != 0 {
		t.Errorf("expected 0 results for non-end events, got %d", len(results))
	}
}

func TestJSONRendererNoOpsDoNotPanic(t *testing.T) {
	r := &JSONRenderer{}

	// These should all be safe no-ops.
	r.Flush()
	r.Finish()
}

func TestJSONRendererImplementsRenderer(t *testing.T) {
	// Compile-time check that JSONRenderer satisfies the Renderer interface.
	var _ Renderer = (*JSONRenderer)(nil)
}

func TestJSONRendererCollectsWarnings(t *testing.T) {
	r := &JSONRenderer{}
	r.HandleEvent(Event{Type: EventWarn, Data: map[string]any{"text": "something is off"}})
	r.HandleEvent(Event{Type: EventWarn, Data: map[string]any{"text": "another warning"}})
	warnings := r.Warnings()
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d", len(warnings))
	}
	if warnings[0] != "something is off" {
		t.Errorf("warnings[0]: got %q, want %q", warnings[0], "something is off")
	}
	if warnings[1] != "another warning" {
		t.Errorf("warnings[1]: got %q, want %q", warnings[1], "another warning")
	}
}

func TestJSONRendererCollectsErrors(t *testing.T) {
	r := &JSONRenderer{}
	r.HandleEvent(Event{Type: EventErr, Data: map[string]any{"text": "something broke"}})
	errs := r.Errors()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if errs[0] != "something broke" {
		t.Errorf("errors[0]: got %q, want %q", errs[0], "something broke")
	}
}

func TestJSONRendererIgnoresPrintEvents(t *testing.T) {
	r := &JSONRenderer{}
	r.HandleEvent(Event{Type: EventPrint, Data: map[string]any{"text": "hello"}})
	if len(r.Results()) != 0 {
		t.Error("expected no results from print event")
	}
	if len(r.Warnings()) != 0 {
		t.Error("expected no warnings from print event")
	}
	if len(r.Errors()) != 0 {
		t.Error("expected no errors from print event")
	}
}

func TestJSONRendererHandlesInvalidData(t *testing.T) {
	r := &JSONRenderer{}

	// Data is not a map[string]any — should be silently ignored.
	r.HandleEvent(Event{
		Type:   EventTaskEnd,
		TS:     time.Now(),
		TaskID: "task-bad",
		Data:   "not a map",
	})

	results := r.Results()
	if len(results) != 0 {
		t.Errorf("expected 0 results for invalid data, got %d", len(results))
	}
}
