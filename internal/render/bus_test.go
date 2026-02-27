package render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBus_RunBatch_ExecutesAllTasks(t *testing.T) {
	var buf bytes.Buffer
	bus := NewBus(&buf, Options{})

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
	bus := NewBus(&buf, Options{})

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
	bus := NewBus(&buf, Options{})

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
	bus := NewBus(&buf, Options{})

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
	bus := NewBus(&output, Options{LogWriter: lw})

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
		// Expect at least: task.start, task.log, task.end
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
		if ev.TaskID == "" {
			t.Errorf("line %d: expected non-empty TaskID", i)
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
