package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLogWriter_WritesJSONL(t *testing.T) {
	var buf bytes.Buffer
	lw := NewLogWriter(&buf)

	for i := range 3 {
		err := lw.Write(Event{
			Type:   EventTaskLog,
			TS:     time.Now(),
			Seq:    uint64(i),
			TaskID: "task-1",
			Data:   map[string]any{"i": i},
		})
		if err != nil {
			t.Fatalf("Write(%d): %v", i, err)
		}
	}

	if err := lw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	for i, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Errorf("line %d is not valid JSON: %s", i, line)
		}
	}
}

func TestLogWriter_PreservesEventFields(t *testing.T) {
	var buf bytes.Buffer
	lw := NewLogWriter(&buf)

	ts := time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC)
	err := lw.Write(Event{
		Type:   EventTaskStart,
		TS:     ts,
		Seq:    42,
		TaskID: "sync-main",
		Data:   map[string]any{"name": "sync"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := lw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	var got struct {
		Type   string         `json:"type"`
		TS     time.Time      `json:"ts"`
		Seq    uint64         `json:"seq"`
		TaskID string         `json:"task_id"`
		Data   map[string]any `json:"data"`
	}

	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Type != EventTaskStart {
		t.Errorf("Type: got %q, want %q", got.Type, EventTaskStart)
	}
	if !got.TS.Equal(ts) {
		t.Errorf("TS: got %v, want %v", got.TS, ts)
	}
	if got.Seq != 42 {
		t.Errorf("Seq: got %d, want 42", got.Seq)
	}
	if got.TaskID != "sync-main" {
		t.Errorf("TaskID: got %q, want %q", got.TaskID, "sync-main")
	}
	if got.Data["name"] != "sync" {
		t.Errorf("Data[name]: got %v, want %q", got.Data["name"], "sync")
	}
}

func TestLogWriter_NoHTMLEscaping(t *testing.T) {
	var buf bytes.Buffer
	lw := NewLogWriter(&buf)

	err := lw.Write(Event{
		Type:   EventTaskLog,
		TS:     time.Now(),
		Seq:    1,
		TaskID: "task-esc",
		Data:   "a < b & c > d",
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := lw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	output := buf.String()

	// With HTML escaping disabled, < > & must appear literally.
	if strings.Contains(output, `\u003c`) {
		t.Errorf("output contains HTML-escaped '<': %s", output)
	}
	if strings.Contains(output, `\u003e`) {
		t.Errorf("output contains HTML-escaped '>': %s", output)
	}
	if strings.Contains(output, `\u0026`) {
		t.Errorf("output contains HTML-escaped '&': %s", output)
	}

	if !strings.Contains(output, "a < b & c > d") {
		t.Errorf("output does not contain literal 'a < b & c > d': %s", output)
	}
}
