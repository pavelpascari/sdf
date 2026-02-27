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
