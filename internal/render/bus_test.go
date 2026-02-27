package render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBus_RunBatch_ExecutesAllTasks(t *testing.T) {
	var buf bytes.Buffer
	bus := NewBus(&buf, io.Discard, Options{})

	var counter atomic.Int32

	for i := range 3 {
		id := string(rune('a' + i))
		bus.AddTask(TaskSpec{
			ID:   id,
			Name: "task-" + id,
			Fn: func(ctx context.Context, r *Reporter) error {
				counter.Add(1)
				r.End("ok", "done")
				return nil
			},
		})
	}

	err := bus.RunBatch(context.Background())
	if err != nil {
		t.Fatalf("RunBatch returned error: %v", err)
	}

	if err := bus.Finish(); err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}

	if got := counter.Load(); got != 3 {
		t.Errorf("counter: got %d, want 3", got)
	}
}

func TestBus_RunBatch_PropagatesError(t *testing.T) {
	var buf bytes.Buffer
	bus := NewBus(&buf, io.Discard, Options{})

	bus.AddTask(TaskSpec{
		ID:   "fail",
		Name: "failing task",
		Fn: func(ctx context.Context, r *Reporter) error {
			return errors.New("something broke")
		},
	})

	err := bus.RunBatch(context.Background())
	if err == nil {
		t.Fatal("RunBatch should have returned an error")
	}
	if !strings.Contains(err.Error(), "something broke") {
		t.Errorf("error message: got %q, want to contain %q", err.Error(), "something broke")
	}

	if err := bus.Finish(); err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}
}

func TestBus_Run_ExecutesSingleTask(t *testing.T) {
	var buf bytes.Buffer
	bus := NewBus(&buf, io.Discard, Options{})

	var ran bool
	err := bus.Run(context.Background(), TaskSpec{
		ID:   "single",
		Name: "single task",
		Fn: func(ctx context.Context, r *Reporter) error {
			ran = true
			r.End("ok", "completed")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if err := bus.Finish(); err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}

	if !ran {
		t.Error("expected task to have run")
	}
}

func TestBus_RunBatch_CancelsOnContext(t *testing.T) {
	var buf bytes.Buffer
	bus := NewBus(&buf, io.Discard, Options{})

	bus.AddTask(TaskSpec{
		ID:   "cancel",
		Name: "canceller",
		Fn: func(ctx context.Context, r *Reporter) error {
			r.End("failed", "canceled")
			return context.Canceled
		},
	})

	err := bus.RunBatch(context.Background())
	if err == nil {
		t.Fatal("RunBatch should have returned an error on cancellation")
	}

	if err := bus.Finish(); err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}
}

func TestBus_LogWriter_ProducesJSONL(t *testing.T) {
	var output bytes.Buffer
	var logBuf bytes.Buffer

	lw := NewLogWriter(&logBuf)
	bus := NewBus(&output, io.Discard, Options{LogWriter: lw})

	bus.AddTask(TaskSpec{
		ID:   "logged",
		Name: "logged task",
		Fn: func(ctx context.Context, r *Reporter) error {
			r.Log("hello from task")
			r.End("ok", "finished")
			return nil
		},
	})

	err := bus.RunBatch(context.Background())
	if err != nil {
		t.Fatalf("RunBatch returned error: %v", err)
	}

	if err := bus.Finish(); err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}

	// Parse the JSONL output — each line should be a valid JSON Event.
	raw := logBuf.String()
	lines := strings.Split(strings.TrimSpace(raw), "\n")

	if len(lines) < 3 {
		// Expect at least: batch.start, task.start, task.log, task.end, batch.end
		t.Fatalf("expected at least 3 JSONL lines, got %d: %q", len(lines), raw)
	}

	for i, line := range lines {
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Errorf("line %d: invalid JSON: %v — %q", i, err, line)
			continue
		}
		if ev.Seq == 0 {
			t.Errorf("line %d: expected non-zero Seq", i)
		}
	}

	// Verify that sequence numbers are monotonically increasing.
	var lastSeq uint64
	for i, line := range lines {
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Seq <= lastSeq {
			t.Errorf("line %d: Seq %d not greater than previous %d", i, ev.Seq, lastSeq)
		}
		lastSeq = ev.Seq
	}
}

func TestBus_PrintWarnErr_EmitEvents(t *testing.T) {
	var logBuf bytes.Buffer

	lw := NewLogWriter(&logBuf)
	bus := NewBus(io.Discard, io.Discard, Options{LogWriter: lw})

	bus.Print("hello")
	bus.Printf("count: %d", 42)
	bus.Warn("watch out")
	bus.Warnf("danger: %s", "fire")
	bus.Err(errors.New("something broke"))

	if err := bus.Finish(); err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}

	raw := logBuf.String()
	lines := strings.Split(strings.TrimSpace(raw), "\n")

	if len(lines) != 5 {
		t.Fatalf("expected 5 JSONL lines, got %d: %q", len(lines), raw)
	}

	// Verify event types and data.
	expected := []struct {
		typ  string
		text string
	}{
		{EventPrint, "hello"},
		{EventPrint, "count: 42"},
		{EventWarn, "watch out"},
		{EventWarn, "danger: fire"},
		{EventErr, "something broke"},
	}

	for i, line := range lines {
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
			continue
		}
		if ev.Type != expected[i].typ {
			t.Errorf("line %d: type = %q, want %q", i, ev.Type, expected[i].typ)
		}
		data, ok := ev.Data.(map[string]any)
		if !ok {
			t.Errorf("line %d: Data is not map[string]any", i)
			continue
		}
		text, _ := data["text"].(string)
		if text != expected[i].text {
			t.Errorf("line %d: text = %q, want %q", i, text, expected[i].text)
		}
		if ev.TaskID != "" {
			t.Errorf("line %d: expected empty TaskID, got %q", i, ev.TaskID)
		}
	}
}

func TestBus_RunThenRunBatch_Interleaved(t *testing.T) {
	var buf bytes.Buffer
	bus := NewBus(&buf, io.Discard, Options{})

	// First: sequential Run
	err := bus.Run(context.Background(), TaskSpec{
		ID:   "seq-1",
		Name: "sequential task",
		Fn: func(ctx context.Context, r *Reporter) error {
			r.Log("step one")
			r.End("ok", "done")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	// Then: parallel RunBatch
	bus.AddTask(TaskSpec{
		ID:   "par-1",
		Name: "parallel task",
		Fn: func(ctx context.Context, r *Reporter) error {
			r.End("ok", "batch done")
			return nil
		},
	})
	err = bus.RunBatch(context.Background())
	if err != nil {
		t.Fatalf("RunBatch returned error: %v", err)
	}

	if err := bus.Finish(); err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "step one") {
		t.Errorf("expected output to contain sequential task log, got:\n%s", out)
	}
}
