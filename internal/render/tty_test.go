package render

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func TestTTYRenderer_BatchMode_ShowsSlots(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf, io.Discard)

	r.HandleEvent(Event{
		Type: EventBatchStart,
		TS:   time.Now(),
		Data: map[string]any{"count": 2, "label": "Running tasks"},
	})
	buf.Reset() // clear the batch start output so we only inspect Flush output

	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"name": "PR #56"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: "t2",
		Data:   map[string]any{"name": "PR #57"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskLog,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"text": "updating title..."},
	})

	r.Flush()

	out := buf.String()
	if !strings.Contains(out, "PR #56") {
		t.Errorf("expected output to contain %q, got:\n%s", "PR #56", out)
	}
	if !strings.Contains(out, "PR #57") {
		t.Errorf("expected output to contain %q, got:\n%s", "PR #57", out)
	}
	if !strings.Contains(out, "updating title...") {
		t.Errorf("expected output to contain %q, got:\n%s", "updating title...", out)
	}
}

func TestTTYRenderer_BatchMode_EndFinalizes(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf, io.Discard)

	r.HandleEvent(Event{
		Type: EventBatchStart,
		TS:   time.Now(),
		Data: map[string]any{"count": 2, "label": "Running tasks"},
	})

	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"name": "PR #56"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: "t2",
		Data:   map[string]any{"name": "PR #57"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskEnd,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"status": "ok", "message": "PR #56: \u2713 unchanged"},
	})

	buf.Reset()
	r.Flush()

	out := buf.String()
	if !strings.Contains(out, "PR #56: \u2713 unchanged") {
		t.Errorf("expected output to contain final message %q, got:\n%s", "PR #56: \u2713 unchanged", out)
	}
}

func TestTTYRenderer_BatchMode_SpinnerCounter(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf, io.Discard)

	r.HandleEvent(Event{
		Type: EventBatchStart,
		TS:   time.Now(),
		Data: map[string]any{"count": 2, "label": "Running tasks"},
	})

	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"name": "PR #56"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: "t2",
		Data:   map[string]any{"name": "PR #57"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskEnd,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"status": "ok", "message": "done"},
	})

	buf.Reset()
	r.Flush()

	out := buf.String()
	if !strings.Contains(out, "1/2") {
		t.Errorf("expected spinner counter to contain %q, got:\n%s", "1/2", out)
	}
}

func TestTTYRenderer_SequentialMode_AppendsLines(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf, io.Discard)

	// No batch.start event — sequential mode by default.

	r.HandleEvent(Event{
		Type:   EventTaskLog,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"text": "rebasing branch..."},
	})
	r.HandleEvent(Event{
		Type:   EventTaskEnd,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"status": "ok", "message": "sync complete"},
	})

	out := buf.String()
	if !strings.Contains(out, "rebasing branch...") {
		t.Errorf("expected output to contain %q, got:\n%s", "rebasing branch...", out)
	}
	if !strings.Contains(out, "sync complete") {
		t.Errorf("expected output to contain %q, got:\n%s", "sync complete", out)
	}

	// Sequential mode should NOT contain ANSI cursor movement sequences.
	if strings.Contains(out, CursorUp(1)) {
		t.Errorf("sequential mode should not use CursorUp, got:\n%s", out)
	}
}

func TestTTYRenderer_PauseShowsCursor(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf, io.Discard)

	r.HandleEvent(Event{
		Type: EventBatchStart,
		TS:   time.Now(),
		Data: map[string]any{"count": 2, "label": "Running tasks"},
	})

	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"name": "PR #56"},
	})

	r.Flush()
	buf.Reset()

	r.HandleEvent(Event{Type: EventPause, TS: time.Now()})

	out := buf.String()
	if !strings.Contains(out, ShowCursor()) {
		t.Errorf("Pause should emit ShowCursor, got:\n%q", out)
	}
}

func TestTTYRenderer_PrintWritesToStdout(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf, io.Discard)

	r.HandleEvent(Event{
		Type: EventPrint,
		TS:   time.Now(),
		Data: map[string]any{"text": "hello world"},
	})

	out := buf.String()
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected stdout to contain %q, got:\n%s", "hello world", out)
	}
}

func TestTTYRenderer_WarnWritesToStderr(t *testing.T) {
	var stdoutBuf, stderrBuf bytes.Buffer
	r := NewTTYRenderer(&stdoutBuf, &stderrBuf)

	r.HandleEvent(Event{
		Type: EventWarn,
		TS:   time.Now(),
		Data: map[string]any{"text": "be careful"},
	})

	if strings.Contains(stdoutBuf.String(), "be careful") {
		t.Error("warn should not write to stdout")
	}
	if !strings.Contains(stderrBuf.String(), "be careful") {
		t.Errorf("expected stderr to contain %q, got:\n%s", "be careful", stderrBuf.String())
	}
}

func TestTTYRenderer_ErrWritesToStderr(t *testing.T) {
	var stdoutBuf, stderrBuf bytes.Buffer
	r := NewTTYRenderer(&stdoutBuf, &stderrBuf)

	r.HandleEvent(Event{
		Type: EventErr,
		TS:   time.Now(),
		Data: map[string]any{"text": "something failed"},
	})

	if strings.Contains(stdoutBuf.String(), "something failed") {
		t.Error("err should not write to stdout")
	}
	if !strings.Contains(stderrBuf.String(), "something failed") {
		t.Errorf("expected stderr to contain %q, got:\n%s", "something failed", stderrBuf.String())
	}
}

func TestTTYRenderer_BatchStartEnd_Lifecycle(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf, io.Discard)

	// Start batch mode.
	r.HandleEvent(Event{
		Type: EventBatchStart,
		TS:   time.Now(),
		Data: map[string]any{"count": 1, "label": "Testing"},
	})

	if !strings.Contains(buf.String(), HideCursor()) {
		t.Error("batch.start should hide cursor")
	}

	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"name": "task one"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskEnd,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"status": "ok", "message": "done"},
	})

	buf.Reset()

	// End batch mode.
	r.HandleEvent(Event{Type: EventBatchEnd, TS: time.Now()})

	out := buf.String()
	if !strings.Contains(out, ShowCursor()) {
		t.Error("batch.end should show cursor")
	}
	if !strings.Contains(out, "done") {
		t.Error("batch.end should flush final state")
	}
}

func TestTTYRenderer_BatchStartFloat64Count(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf, io.Discard)

	// Simulate JSON-unmarshalled count (float64 instead of int).
	r.HandleEvent(Event{
		Type: EventBatchStart,
		TS:   time.Now(),
		Data: map[string]any{"count": float64(2), "label": "Tasks"},
	})

	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"name": "task one"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: "t2",
		Data:   map[string]any{"name": "task two"},
	})

	buf.Reset()
	r.Flush()

	out := buf.String()
	if !strings.Contains(out, "0/2") {
		t.Errorf("expected spinner counter to show 0/2, got:\n%s", out)
	}
}

func TestTTYRenderer_BatchComplete_ShowsCheckmark(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf, io.Discard)

	r.HandleEvent(Event{
		Type: EventBatchStart,
		TS:   time.Now(),
		Data: map[string]any{"count": 2, "label": "Running tasks"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"name": "task one"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: "t2",
		Data:   map[string]any{"name": "task two"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskEnd,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"status": "ok", "message": "done 1"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskEnd,
		TS:     time.Now(),
		TaskID: "t2",
		Data:   map[string]any{"status": "ok", "message": "done 2"},
	})

	buf.Reset()
	r.Flush()

	out := buf.String()
	if !strings.Contains(out, "✓ Running tasks (2/2)") {
		t.Errorf("expected checkmark when all tasks complete, got:\n%q", out)
	}
}

func TestTTYRenderer_ImplementsRenderer(t *testing.T) {
	// Compile-time check that TTYRenderer satisfies the Renderer interface.
	var _ Renderer = (*TTYRenderer)(nil)
}
