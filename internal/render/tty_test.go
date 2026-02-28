package render

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestTTYRenderer_BatchMode_ShowsSlots(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf)
	r.Init(2)
	buf.Reset() // clear the Init output so we only inspect Flush output

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
	r := NewTTYRenderer(&buf)
	r.Init(2)

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
	r := NewTTYRenderer(&buf)
	r.Init(2)

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
	r := NewTTYRenderer(&buf)
	r.Init(0)

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
	r := NewTTYRenderer(&buf)
	r.Init(2)

	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: "t1",
		Data:   map[string]any{"name": "PR #56"},
	})

	r.Flush()
	buf.Reset()

	r.Pause()

	out := buf.String()
	if !strings.Contains(out, ShowCursor()) {
		t.Errorf("Pause should emit ShowCursor, got:\n%q", out)
	}
}

func TestTTYRenderer_ImplementsRenderer(t *testing.T) {
	// Compile-time check that TTYRenderer satisfies the Renderer interface.
	var _ Renderer = (*TTYRenderer)(nil)
}
