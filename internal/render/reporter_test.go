package render

import (
	"testing"
	"time"
)

func newTestChannel() chan Event {
	return make(chan Event, 10)
}

func TestReporterStart(t *testing.T) {
	ch := newTestChannel()
	r := NewReporter("task-1", ch)

	before := time.Now()
	r.Start("sync branches")
	after := time.Now()

	if len(ch) != 1 {
		t.Fatalf("expected 1 event, got %d", len(ch))
	}

	e := <-ch
	if e.Type != EventTaskStart {
		t.Errorf("Type: got %q, want %q", e.Type, EventTaskStart)
	}
	if e.TaskID != "task-1" {
		t.Errorf("TaskID: got %q, want %q", e.TaskID, "task-1")
	}
	if e.TS.Before(before) || e.TS.After(after) {
		t.Errorf("TS %v not in range [%v, %v]", e.TS, before, after)
	}

	data, ok := e.Data.(map[string]any)
	if !ok {
		t.Fatal("Data: expected map[string]any")
	}
	if data["name"] != "sync branches" {
		t.Errorf("Data[name]: got %v, want %q", data["name"], "sync branches")
	}
}

func TestReporterLog(t *testing.T) {
	ch := newTestChannel()
	r := NewReporter("task-2", ch)

	r.Log("rebasing onto main")

	if len(ch) != 1 {
		t.Fatalf("expected 1 event, got %d", len(ch))
	}

	e := <-ch
	if e.Type != EventTaskLog {
		t.Errorf("Type: got %q, want %q", e.Type, EventTaskLog)
	}
	if e.TaskID != "task-2" {
		t.Errorf("TaskID: got %q, want %q", e.TaskID, "task-2")
	}

	data, ok := e.Data.(map[string]any)
	if !ok {
		t.Fatal("Data: expected map[string]any")
	}
	if data["text"] != "rebasing onto main" {
		t.Errorf("Data[text]: got %v, want %q", data["text"], "rebasing onto main")
	}
}

func TestReporterEnd(t *testing.T) {
	ch := newTestChannel()
	r := NewReporter("task-3", ch)

	r.End("ok", "done successfully")

	if len(ch) != 1 {
		t.Fatalf("expected 1 event, got %d", len(ch))
	}

	e := <-ch
	if e.Type != EventTaskEnd {
		t.Errorf("Type: got %q, want %q", e.Type, EventTaskEnd)
	}
	if e.TaskID != "task-3" {
		t.Errorf("TaskID: got %q, want %q", e.TaskID, "task-3")
	}

	data, ok := e.Data.(map[string]any)
	if !ok {
		t.Fatal("Data: expected map[string]any")
	}
	if data["status"] != "ok" {
		t.Errorf("Data[status]: got %v, want %q", data["status"], "ok")
	}
	if data["message"] != "done successfully" {
		t.Errorf("Data[message]: got %v, want %q", data["message"], "done successfully")
	}
}

func TestReporterPause(t *testing.T) {
	ch := newTestChannel()
	r := NewReporter("task-4", ch)

	r.Pause()

	if len(ch) != 1 {
		t.Fatalf("expected 1 event, got %d", len(ch))
	}

	e := <-ch
	if e.Type != EventPause {
		t.Errorf("Type: got %q, want %q", e.Type, EventPause)
	}
	if e.TaskID != "task-4" {
		t.Errorf("TaskID: got %q, want %q", e.TaskID, "task-4")
	}
	if e.Data != nil {
		t.Errorf("Data: expected nil, got %v", e.Data)
	}
}

func TestReporterResume(t *testing.T) {
	ch := newTestChannel()
	r := NewReporter("task-5", ch)

	r.Resume()

	if len(ch) != 1 {
		t.Fatalf("expected 1 event, got %d", len(ch))
	}

	e := <-ch
	if e.Type != EventResume {
		t.Errorf("Type: got %q, want %q", e.Type, EventResume)
	}
	if e.TaskID != "task-5" {
		t.Errorf("TaskID: got %q, want %q", e.TaskID, "task-5")
	}
	if e.Data != nil {
		t.Errorf("Data: expected nil, got %v", e.Data)
	}
}
