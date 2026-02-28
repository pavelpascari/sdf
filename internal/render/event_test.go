package render

import (
	"testing"
	"time"
)

func TestEventConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"EventTaskStart", EventTaskStart, "task.start"},
		{"EventTaskLog", EventTaskLog, "task.log"},
		{"EventTaskEnd", EventTaskEnd, "task.end"},
		{"EventPause", EventPause, "renderer.pause"},
		{"EventResume", EventResume, "renderer.resume"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, want %q", tt.got, tt.expected)
			}
		})
	}
}

func TestEventFields(t *testing.T) {
	now := time.Now()
	e := Event{
		Type:   EventTaskStart,
		TS:     now,
		Seq:    42,
		TaskID: "task-1",
		Data:   map[string]any{"name": "sync"},
	}

	if e.Type != EventTaskStart {
		t.Errorf("Type: got %q, want %q", e.Type, EventTaskStart)
	}
	if !e.TS.Equal(now) {
		t.Errorf("TS: got %v, want %v", e.TS, now)
	}
	if e.Seq != 42 {
		t.Errorf("Seq: got %d, want 42", e.Seq)
	}
	if e.TaskID != "task-1" {
		t.Errorf("TaskID: got %q, want %q", e.TaskID, "task-1")
	}

	data, ok := e.Data.(map[string]any)
	if !ok {
		t.Fatal("Data: expected map[string]any")
	}
	if data["name"] != "sync" {
		t.Errorf("Data[name]: got %v, want %q", data["name"], "sync")
	}
}

func TestEventDataNilByDefault(t *testing.T) {
	var e Event
	if e.Data != nil {
		t.Errorf("Data: expected nil, got %v", e.Data)
	}
}
