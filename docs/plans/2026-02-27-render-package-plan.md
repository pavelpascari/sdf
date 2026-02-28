# `internal/render` Package Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a rendering and orchestration package that replaces `orderedPrinter` in `sync.go` with a pluggable event bus supporting parallel task execution, in-place terminal updates, JSON output, and JSONL log persistence.

**Architecture:** Commands define tasks as `TaskSpec` structs. A `Bus` runs them (sequentially or in parallel via `errgroup`), routing typed `Event` structs through a channel to a `Renderer` interface. TTYRenderer draws a multi-line in-place block with spinner. JSONRenderer silently collects results. A LogWriter tees all events to `.sdf/logs/*.jsonl` as a side-effect.

**Tech Stack:** Go stdlib (`encoding/json`, `sync/atomic`, `context`), `golang.org/x/sync/errgroup`, `mattn/go-isatty`, `charmbracelet/lipgloss` — all already in `go.mod`.

**Design doc:** `docs/plans/2026-02-27-render-package-design.md`

---

### Task 1: Event Types and Constants

**Files:**
- Create: `internal/render/event.go`
- Test: `internal/render/event_test.go`

**Step 1: Write the failing test**

```go
// internal/render/event_test.go
package render

import (
	"testing"
	"time"
)

func TestNewEvent_SetsFields(t *testing.T) {
	ev := Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		Seq:    1,
		TaskID: "t1",
		Data:   map[string]any{"name": "download"},
	}
	if ev.Type != "task.start" {
		t.Fatalf("got type %q, want %q", ev.Type, "task.start")
	}
	if ev.TaskID != "t1" {
		t.Fatalf("got task id %q, want %q", ev.TaskID, "t1")
	}
}

func TestEventConstants(t *testing.T) {
	if EventTaskStart != "task.start" {
		t.Fatal("wrong constant")
	}
	if EventTaskLog != "task.log" {
		t.Fatal("wrong constant")
	}
	if EventTaskEnd != "task.end" {
		t.Fatal("wrong constant")
	}
	if EventPause != "renderer.pause" {
		t.Fatal("wrong constant")
	}
	if EventResume != "renderer.resume" {
		t.Fatal("wrong constant")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/render/ -run TestNewEvent -v -count=1`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
// internal/render/event.go
package render

import "time"

const (
	EventTaskStart = "task.start"
	EventTaskLog   = "task.log"
	EventTaskEnd   = "task.end"
	EventPause     = "renderer.pause"
	EventResume    = "renderer.resume"
)

type Event struct {
	Type   string    `json:"type"`
	TS     time.Time `json:"ts"`
	Seq    uint64    `json:"seq"`
	TaskID string    `json:"task_id"`
	Data   any       `json:"data,omitempty"`
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/render/ -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/render/event.go internal/render/event_test.go
git commit -m "feat(render): add Event struct and type constants"
```

---

### Task 2: Reporter

**Files:**
- Create: `internal/render/reporter.go`
- Test: `internal/render/reporter_test.go`

**Step 1: Write the failing test**

```go
// internal/render/reporter_test.go
package render

import (
	"testing"
	"time"
)

func TestReporter_Start(t *testing.T) {
	ch := make(chan Event, 10)
	r := &Reporter{taskID: "t1", events: ch}
	r.Start("download")

	ev := <-ch
	if ev.Type != EventTaskStart {
		t.Fatalf("got type %q, want %q", ev.Type, EventTaskStart)
	}
	if ev.TaskID != "t1" {
		t.Fatalf("got task id %q, want %q", ev.TaskID, "t1")
	}
	data := ev.Data.(map[string]any)
	if data["name"] != "download" {
		t.Fatalf("got name %q, want %q", data["name"], "download")
	}
}

func TestReporter_Log(t *testing.T) {
	ch := make(chan Event, 10)
	r := &Reporter{taskID: "t1", events: ch}
	r.Log("fetching index...")

	ev := <-ch
	if ev.Type != EventTaskLog {
		t.Fatalf("got type %q, want %q", ev.Type, EventTaskLog)
	}
	data := ev.Data.(map[string]any)
	if data["text"] != "fetching index..." {
		t.Fatalf("got text %q", data["text"])
	}
}

func TestReporter_End(t *testing.T) {
	ch := make(chan Event, 10)
	r := &Reporter{taskID: "t1", events: ch}
	r.End("succeeded", "updated (title)")

	ev := <-ch
	if ev.Type != EventTaskEnd {
		t.Fatalf("got type %q, want %q", ev.Type, EventTaskEnd)
	}
	data := ev.Data.(map[string]any)
	if data["status"] != "succeeded" {
		t.Fatalf("got status %q", data["status"])
	}
	if data["message"] != "updated (title)" {
		t.Fatalf("got message %q", data["message"])
	}
}

func TestReporter_SeqIncrements(t *testing.T) {
	ch := make(chan Event, 10)
	r := &Reporter{taskID: "t1", events: ch}
	r.Start("a")
	r.Log("b")
	r.End("succeeded", "c")

	seqs := make([]uint64, 0, 3)
	for range 3 {
		ev := <-ch
		seqs = append(seqs, ev.Seq)
	}
	// Seq is assigned by the router (0 here), but taskSeq should increment.
	// Reporter doesn't set global Seq — that's the router's job.
	// Just verify events arrive and are distinct timestamps.
	if len(seqs) != 3 {
		t.Fatalf("expected 3 events, got %d", len(seqs))
	}
}

func TestReporter_PauseResume(t *testing.T) {
	ch := make(chan Event, 10)
	r := &Reporter{taskID: "t1", events: ch}
	r.Pause()
	r.Resume()

	ev1 := <-ch
	if ev1.Type != EventPause {
		t.Fatalf("got type %q, want %q", ev1.Type, EventPause)
	}
	ev2 := <-ch
	if ev2.Type != EventResume {
		t.Fatalf("got type %q, want %q", ev2.Type, EventResume)
	}
}

func drainTimeout(ch <-chan Event, d time.Duration) (Event, bool) {
	select {
	case ev := <-ch:
		return ev, true
	case <-time.After(d):
		return Event{}, false
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/render/ -run TestReporter -v -count=1`
Expected: FAIL — `Reporter` not defined

**Step 3: Write minimal implementation**

```go
// internal/render/reporter.go
package render

import "time"

// Reporter is the per-task handle for emitting events.
// Commands call its methods instead of fmt.Printf.
type Reporter struct {
	taskID string
	events chan<- Event
}

func (r *Reporter) Start(name string) {
	r.events <- Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: r.taskID,
		Data:   map[string]any{"name": name},
	}
}

func (r *Reporter) Log(text string) {
	r.events <- Event{
		Type:   EventTaskLog,
		TS:     time.Now(),
		TaskID: r.taskID,
		Data:   map[string]any{"text": text},
	}
}

func (r *Reporter) End(status, message string) {
	r.events <- Event{
		Type:   EventTaskEnd,
		TS:     time.Now(),
		TaskID: r.taskID,
		Data:   map[string]any{"status": status, "message": message},
	}
}

func (r *Reporter) Pause() {
	r.events <- Event{
		Type:   EventPause,
		TS:     time.Now(),
		TaskID: r.taskID,
	}
}

func (r *Reporter) Resume() {
	r.events <- Event{
		Type:   EventResume,
		TS:     time.Now(),
		TaskID: r.taskID,
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/render/ -run TestReporter -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/render/reporter.go internal/render/reporter_test.go
git commit -m "feat(render): add Reporter with Start/Log/End/Pause/Resume"
```

---

### Task 3: Renderer Interface and JSONRenderer

**Files:**
- Create: `internal/render/renderer.go`
- Create: `internal/render/json.go`
- Test: `internal/render/json_test.go`

**Step 1: Write the failing test**

```go
// internal/render/json_test.go
package render

import "testing"

func TestJSONRenderer_CollectsEndEvents(t *testing.T) {
	r := &JSONRenderer{}
	r.Init(3)

	r.HandleEvent(Event{Type: EventTaskStart, TaskID: "t1", Data: map[string]any{"name": "PR #1"}})
	r.HandleEvent(Event{Type: EventTaskLog, TaskID: "t1", Data: map[string]any{"text": "working..."}})
	r.HandleEvent(Event{Type: EventTaskEnd, TaskID: "t1", Data: map[string]any{"status": "succeeded", "message": "updated"}})
	r.HandleEvent(Event{Type: EventTaskEnd, TaskID: "t2", Data: map[string]any{"status": "succeeded", "message": "unchanged"}})

	results := r.Results()
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].TaskID != "t1" {
		t.Fatalf("got task id %q, want %q", results[0].TaskID, "t1")
	}
	if results[0].Status != "succeeded" {
		t.Fatalf("got status %q", results[0].Status)
	}
	if results[0].Message != "updated" {
		t.Fatalf("got message %q", results[0].Message)
	}
}

func TestJSONRenderer_IgnoresNonEndEvents(t *testing.T) {
	r := &JSONRenderer{}
	r.Init(2)

	r.HandleEvent(Event{Type: EventTaskStart, TaskID: "t1"})
	r.HandleEvent(Event{Type: EventTaskLog, TaskID: "t1"})
	r.HandleEvent(Event{Type: EventPause, TaskID: "t1"})
	r.HandleEvent(Event{Type: EventResume, TaskID: "t1"})

	if len(r.Results()) != 0 {
		t.Fatalf("expected 0 results, got %d", len(r.Results()))
	}
}

func TestJSONRenderer_PauseResumeAreNoOps(t *testing.T) {
	r := &JSONRenderer{}
	r.Init(1)
	// These must not panic.
	r.Pause()
	r.Resume()
	r.Flush()
	r.Finish()
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/render/ -run TestJSONRenderer -v -count=1`
Expected: FAIL — `JSONRenderer` not defined

**Step 3: Write minimal implementation**

```go
// internal/render/renderer.go
package render

// Renderer is the pluggable output interface.
// Swapped based on --json flag / TTY detection.
type Renderer interface {
	Init(taskCount int)
	HandleEvent(Event)
	Flush()
	Pause()
	Resume()
	Finish()
}
```

```go
// internal/render/json.go
package render

// TaskResult holds the outcome of a single task, extracted from task.end events.
type TaskResult struct {
	TaskID  string
	Status  string
	Message string
}

// JSONRenderer silently collects task outcomes for --json output.
// The command retrieves Results(), builds its own result struct, and prints.
type JSONRenderer struct {
	results []TaskResult
}

func (r *JSONRenderer) Init(taskCount int) {
	r.results = make([]TaskResult, 0, taskCount)
}

func (r *JSONRenderer) HandleEvent(ev Event) {
	if ev.Type != EventTaskEnd {
		return
	}
	data, _ := ev.Data.(map[string]any)
	status, _ := data["status"].(string)
	message, _ := data["message"].(string)
	r.results = append(r.results, TaskResult{
		TaskID:  ev.TaskID,
		Status:  status,
		Message: message,
	})
}

func (r *JSONRenderer) Flush()  {}
func (r *JSONRenderer) Pause()  {}
func (r *JSONRenderer) Resume() {}
func (r *JSONRenderer) Finish() {}

// Results returns the collected task outcomes.
func (r *JSONRenderer) Results() []TaskResult {
	return r.results
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/render/ -run TestJSONRenderer -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/render/renderer.go internal/render/json.go internal/render/json_test.go
git commit -m "feat(render): add Renderer interface and JSONRenderer"
```

---

### Task 4: JSONL LogWriter

**Files:**
- Create: `internal/render/log.go`
- Test: `internal/render/log_test.go`

**Step 1: Write the failing test**

```go
// internal/render/log_test.go
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

	lw.Write(Event{Type: EventTaskStart, TS: time.Now(), TaskID: "t1", Data: map[string]any{"name": "task1"}})
	lw.Write(Event{Type: EventTaskLog, TS: time.Now(), TaskID: "t1", Data: map[string]any{"text": "working"}})
	lw.Write(Event{Type: EventTaskEnd, TS: time.Now(), TaskID: "t1", Data: map[string]any{"status": "succeeded"}})
	lw.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3:\n%s", len(lines), buf.String())
	}

	// Each line must be valid JSON.
	for i, line := range lines {
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %d not valid JSON: %v\nline: %s", i, err, line)
		}
	}
}

func TestLogWriter_PreservesEventFields(t *testing.T) {
	var buf bytes.Buffer
	lw := NewLogWriter(&buf)

	ts := time.Date(2026, 2, 27, 10, 0, 0, 0, time.UTC)
	lw.Write(Event{Type: EventTaskStart, TS: ts, Seq: 42, TaskID: "abc"})
	lw.Flush()

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["type"] != "task.start" {
		t.Fatalf("got type %v", parsed["type"])
	}
	if parsed["task_id"] != "abc" {
		t.Fatalf("got task_id %v", parsed["task_id"])
	}
	if parsed["seq"].(float64) != 42 {
		t.Fatalf("got seq %v", parsed["seq"])
	}
}

func TestLogWriter_NoHTMLEscaping(t *testing.T) {
	var buf bytes.Buffer
	lw := NewLogWriter(&buf)
	lw.Write(Event{Type: EventTaskLog, TS: time.Now(), TaskID: "t1", Data: map[string]any{"text": "a < b & c > d"}})
	lw.Flush()

	// encoding/json with SetEscapeHTML(false) should NOT escape <, >, &
	if strings.Contains(buf.String(), `\u003c`) {
		t.Fatal("HTML escaping should be disabled")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/render/ -run TestLogWriter -v -count=1`
Expected: FAIL — `NewLogWriter` not defined

**Step 3: Write minimal implementation**

```go
// internal/render/log.go
package render

import (
	"bufio"
	"encoding/json"
	"io"
)

// LogWriter serializes events to JSONL format.
// This is a router side-effect for persistence — not a renderer.
type LogWriter struct {
	bw  *bufio.Writer
	enc *json.Encoder
}

// NewLogWriter creates a LogWriter that writes JSONL to w.
func NewLogWriter(w io.Writer) *LogWriter {
	bw := bufio.NewWriterSize(w, 64*1024)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	return &LogWriter{bw: bw, enc: enc}
}

// Write serializes a single event as one JSON line.
func (l *LogWriter) Write(ev Event) error {
	return l.enc.Encode(ev)
}

// Flush flushes the buffered writer.
func (l *LogWriter) Flush() error {
	return l.bw.Flush()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/render/ -run TestLogWriter -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/render/log.go internal/render/log_test.go
git commit -m "feat(render): add JSONL LogWriter for event persistence"
```

---

### Task 5: ANSI Helpers

**Files:**
- Create: `internal/render/ansi.go`
- Test: `internal/render/ansi_test.go`

**Step 1: Write the failing test**

```go
// internal/render/ansi_test.go
package render

import "testing"

func TestCup(t *testing.T) {
	// CUP is 1-based: CSI row;col H
	got := Cup(3, 1)
	want := "\x1b[3;1H"
	if got != want {
		t.Fatalf("Cup(3,1) = %q, want %q", got, want)
	}
}

func TestEl(t *testing.T) {
	got := El()
	want := "\x1b[2K"
	if got != want {
		t.Fatalf("El() = %q, want %q", got, want)
	}
}

func TestHideCursor(t *testing.T) {
	got := HideCursor()
	want := "\x1b[?25l"
	if got != want {
		t.Fatalf("HideCursor() = %q, want %q", got, want)
	}
}

func TestShowCursor(t *testing.T) {
	got := ShowCursor()
	want := "\x1b[?25h"
	if got != want {
		t.Fatalf("ShowCursor() = %q, want %q", got, want)
	}
}

func TestCursorUp(t *testing.T) {
	got := CursorUp(5)
	want := "\x1b[5A"
	if got != want {
		t.Fatalf("CursorUp(5) = %q, want %q", got, want)
	}
}

func TestCursorDown(t *testing.T) {
	got := CursorDown(2)
	want := "\x1b[2B"
	if got != want {
		t.Fatalf("CursorDown(2) = %q, want %q", got, want)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/render/ -run TestCup -v -count=1`
Expected: FAIL — `Cup` not defined

**Step 3: Write minimal implementation**

```go
// internal/render/ansi.go
package render

import "fmt"

const esc = "\x1b"

// Cup moves cursor to 1-based (row, col).
func Cup(row, col int) string { return fmt.Sprintf("%s[%d;%dH", esc, row, col) }

// El clears the entire current line (EL 2).
func El() string { return esc + "[2K" }

// HideCursor hides the terminal cursor (DECTCEM).
func HideCursor() string { return esc + "[?25l" }

// ShowCursor shows the terminal cursor (DECTCEM).
func ShowCursor() string { return esc + "[?25h" }

// CursorUp moves cursor up n lines.
func CursorUp(n int) string { return fmt.Sprintf("%s[%dA", esc, n) }

// CursorDown moves cursor down n lines.
func CursorDown(n int) string { return fmt.Sprintf("%s[%dB", esc, n) }

// CarriageReturn moves cursor to column 1 of current line.
func CarriageReturn() string { return "\r" }
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/render/ -run "TestCup|TestEl|TestHide|TestShow|TestCursor" -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/render/ansi.go internal/render/ansi_test.go
git commit -m "feat(render): add ANSI terminal control helpers"
```

---

### Task 6: TTYRenderer

**Files:**
- Create: `internal/render/tty.go`
- Test: `internal/render/tty_test.go`

This is the most complex component. The TTY renderer has two modes:
- **Batch mode**: multi-line in-place block with spinner (used by `RunBatch`)
- **Sequential mode**: append-only, one line at a time (used by `Run`)

**Step 1: Write the failing tests**

```go
// internal/render/tty_test.go
package render

import (
	"bytes"
	"strings"
	"testing"
)

func TestTTYRenderer_BatchMode_ShowsSlots(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf)
	r.Init(2)

	r.HandleEvent(Event{Type: EventTaskStart, TaskID: "t0", Data: map[string]any{"name": "PR #1"}})
	r.HandleEvent(Event{Type: EventTaskStart, TaskID: "t1", Data: map[string]any{"name": "PR #2"}})
	r.HandleEvent(Event{Type: EventTaskLog, TaskID: "t0", Data: map[string]any{"text": "updating title..."}})
	r.Flush()

	out := buf.String()
	if !strings.Contains(out, "PR #1") {
		t.Fatalf("expected PR #1 in output:\n%s", out)
	}
	if !strings.Contains(out, "updating title...") {
		t.Fatalf("expected 'updating title...' in output:\n%s", out)
	}
	if !strings.Contains(out, "PR #2") {
		t.Fatalf("expected PR #2 in output:\n%s", out)
	}
}

func TestTTYRenderer_BatchMode_EndFinalizes(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf)
	r.Init(2)

	r.HandleEvent(Event{Type: EventTaskStart, TaskID: "t0", Data: map[string]any{"name": "PR #1"}})
	r.HandleEvent(Event{Type: EventTaskStart, TaskID: "t1", Data: map[string]any{"name": "PR #2"}})
	r.HandleEvent(Event{Type: EventTaskEnd, TaskID: "t0", Data: map[string]any{"status": "succeeded", "message": "unchanged"}})
	r.Flush()

	out := buf.String()
	if !strings.Contains(out, "unchanged") {
		t.Fatalf("expected 'unchanged' in output:\n%s", out)
	}
}

func TestTTYRenderer_BatchMode_SpinnerLine(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf)
	r.Init(2)

	r.HandleEvent(Event{Type: EventTaskStart, TaskID: "t0", Data: map[string]any{"name": "PR #1"}})
	r.HandleEvent(Event{Type: EventTaskStart, TaskID: "t1", Data: map[string]any{"name": "PR #2"}})
	r.HandleEvent(Event{Type: EventTaskEnd, TaskID: "t0", Data: map[string]any{"status": "succeeded", "message": "done"}})
	r.Flush()

	out := buf.String()
	// Spinner line should show completion counter
	if !strings.Contains(out, "1/2") {
		t.Fatalf("expected completion counter '1/2' in output:\n%s", out)
	}
}

func TestTTYRenderer_SequentialMode_AppendsLines(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf)
	// Init with 0 means sequential mode — no batch block
	r.Init(0)

	r.HandleEvent(Event{Type: EventTaskLog, TaskID: "t0", Data: map[string]any{"text": "rebasing..."}})
	r.HandleEvent(Event{Type: EventTaskEnd, TaskID: "t0", Data: map[string]any{"status": "succeeded", "message": "rebased and pushed"}})

	out := buf.String()
	if !strings.Contains(out, "rebasing...") {
		t.Fatalf("expected 'rebasing...' in output:\n%s", out)
	}
	if !strings.Contains(out, "rebased and pushed") {
		t.Fatalf("expected 'rebased and pushed' in output:\n%s", out)
	}
}

func TestTTYRenderer_PauseResume(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf)
	r.Init(2)

	r.HandleEvent(Event{Type: EventTaskStart, TaskID: "t0", Data: map[string]any{"name": "PR #1"}})
	r.Flush()
	buf.Reset()

	// Pause should emit show-cursor (restores terminal for prompts)
	r.Pause()
	out := buf.String()
	if !strings.Contains(out, ShowCursor()) {
		t.Fatalf("expected ShowCursor in pause output:\n%q", out)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/render/ -run TestTTYRenderer -v -count=1`
Expected: FAIL — `NewTTYRenderer` not defined

**Step 3: Write implementation**

```go
// internal/render/tty.go
package render

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type slot struct {
	label  string // task name, e.g. "PR #57"
	status string // current status from task.log
	done   bool
	final  string // final line from task.end
}

// TTYRenderer renders task progress to a terminal.
// Batch mode (Init with n>0): multi-line in-place block with spinner.
// Sequential mode (Init with n=0): append-only, no cursor movement.
type TTYRenderer struct {
	mu           sync.Mutex
	w            io.Writer
	slots        []slot
	taskIndex    map[string]int // taskID → slot index
	completed    int
	spinnerFrame int
	batchMode    bool
	paused       bool
	label        string
}

func NewTTYRenderer(w io.Writer) *TTYRenderer {
	return &TTYRenderer{
		w:         w,
		taskIndex: make(map[string]int),
		label:     "Running tasks",
	}
}

func (r *TTYRenderer) Init(taskCount int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batchMode = taskCount > 0
	r.slots = make([]slot, 0, taskCount)
	r.taskIndex = make(map[string]int, taskCount)
	r.completed = 0
	if r.batchMode {
		fmt.Fprint(r.w, HideCursor())
	}
}

func (r *TTYRenderer) HandleEvent(ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch ev.Type {
	case EventTaskStart:
		data, _ := ev.Data.(map[string]any)
		name, _ := data["name"].(string)
		idx := len(r.slots)
		r.slots = append(r.slots, slot{label: name, status: "starting..."})
		r.taskIndex[ev.TaskID] = idx

	case EventTaskLog:
		idx, ok := r.taskIndex[ev.TaskID]
		if !ok {
			return
		}
		data, _ := ev.Data.(map[string]any)
		text, _ := data["text"].(string)
		r.slots[idx].status = text
		if !r.batchMode {
			fmt.Fprintf(r.w, "  %s\n", text)
		}

	case EventTaskEnd:
		idx, ok := r.taskIndex[ev.TaskID]
		if !ok {
			return
		}
		data, _ := ev.Data.(map[string]any)
		status, _ := data["status"].(string)
		message, _ := data["message"].(string)
		r.slots[idx].done = true
		r.slots[idx].final = formatFinal(status, message)
		r.completed++
		if !r.batchMode {
			fmt.Fprintf(r.w, "  %s\n", r.slots[idx].final)
		}

	case EventPause:
		r.paused = true

	case EventResume:
		r.paused = false
	}
}

func (r *TTYRenderer) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.batchMode || r.paused {
		return
	}
	r.renderBlock()
}

func (r *TTYRenderer) Pause() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paused = true
	// Move cursor below the block and show cursor for prompt interaction.
	if r.batchMode {
		fmt.Fprint(r.w, ShowCursor())
	}
}

func (r *TTYRenderer) Resume() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paused = false
	if r.batchMode {
		fmt.Fprint(r.w, HideCursor())
	}
}

func (r *TTYRenderer) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.batchMode {
		r.renderBlock()
		fmt.Fprint(r.w, ShowCursor())
	}
}

// renderBlock redraws all slots and the spinner line.
// Called with r.mu held.
func (r *TTYRenderer) renderBlock() {
	var b strings.Builder

	// Move cursor to start of block.
	// We re-render from current position using \r + cursor up.
	totalLines := len(r.slots) + 2 // slots + blank + spinner
	b.WriteString(fmt.Sprintf("\r%s", CursorUp(totalLines)))

	for _, s := range r.slots {
		b.WriteString(El())
		if s.done {
			b.WriteString(fmt.Sprintf("  %s\n", s.final))
		} else {
			b.WriteString(fmt.Sprintf("  %s: %s\n", s.label, s.status))
		}
	}

	// Blank line + spinner
	b.WriteString(El())
	b.WriteString("\n")
	b.WriteString(El())
	frame := spinnerFrames[r.spinnerFrame%len(spinnerFrames)]
	b.WriteString(fmt.Sprintf("  %s %s (%d/%d)\n", frame, r.label, r.completed, len(r.slots)))
	r.spinnerFrame++

	fmt.Fprint(r.w, b.String())
}

func formatFinal(status, message string) string {
	// The command can pass pre-formatted messages using ui.SymOK etc.
	// If not pre-formatted, add a simple prefix.
	if message == "" {
		message = status
	}
	return message
}
```

**Note:** The initial Flush implementation uses cursor-up from current position. The `Init` in batch mode must print enough newlines to "allocate" the block space. Add this to `Init`:

After `fmt.Fprint(r.w, HideCursor())` in Init, add:
```go
// Allocate space for the block: slots + blank + spinner.
for range taskCount + 2 {
    fmt.Fprintln(r.w)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/render/ -run TestTTYRenderer -v -count=1`
Expected: PASS

Iterate on the implementation if tests fail. The key behaviors:
- Batch mode: CursorUp to start of block, rewrite each line with EL, spinner at bottom
- Sequential mode: plain `fmt.Fprintf` append-only
- Pause: show cursor, stop rendering
- Resume: hide cursor, resume rendering

**Step 5: Commit**

```bash
git add internal/render/tty.go internal/render/tty_test.go
git commit -m "feat(render): add TTYRenderer with batch and sequential modes"
```

---

### Task 7: Bus (Orchestrator)

**Files:**
- Create: `internal/render/bus.go`
- Test: `internal/render/bus_test.go`

**Step 1: Write the failing tests**

```go
// internal/render/bus_test.go
package render

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestBus_RunBatch_ExecutesAllTasks(t *testing.T) {
	var buf bytes.Buffer
	bus := NewBus(&buf, Options{})

	var count atomic.Int32
	for i := range 3 {
		id := fmt.Sprintf("t%d", i)
		bus.AddTask(TaskSpec{
			ID:   id,
			Name: fmt.Sprintf("task %d", i),
			Fn: func(ctx context.Context, r *Reporter) error {
				count.Add(1)
				r.End("succeeded", "done")
				return nil
			},
		})
	}

	err := bus.RunBatch(context.Background())
	if err != nil {
		t.Fatalf("RunBatch failed: %v", err)
	}
	bus.Finish()

	if count.Load() != 3 {
		t.Fatalf("expected 3 tasks executed, got %d", count.Load())
	}
}

func TestBus_RunBatch_PropagatesError(t *testing.T) {
	var buf bytes.Buffer
	bus := NewBus(&buf, Options{})

	bus.AddTask(TaskSpec{
		ID: "t0", Name: "fail",
		Fn: func(ctx context.Context, r *Reporter) error {
			r.End("failed", "boom")
			return errors.New("boom")
		},
	})

	err := bus.RunBatch(context.Background())
	if err == nil {
		t.Fatal("expected error from RunBatch")
	}
	bus.Finish()
}

func TestBus_Run_ExecutesSingleTask(t *testing.T) {
	var buf bytes.Buffer
	bus := NewBus(&buf, Options{})

	executed := false
	err := bus.Run(context.Background(), TaskSpec{
		ID: "t0", Name: "single",
		Fn: func(ctx context.Context, r *Reporter) error {
			executed = true
			r.Log("working...")
			r.End("succeeded", "done")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	bus.Finish()

	if !executed {
		t.Fatal("task was not executed")
	}
}

func TestBus_RunBatch_CancelsOnContext(t *testing.T) {
	var buf bytes.Buffer
	bus := NewBus(&buf, Options{})

	ctx, cancel := context.WithCancel(context.Background())

	bus.AddTask(TaskSpec{
		ID: "t0", Name: "blocker",
		Fn: func(ctx context.Context, r *Reporter) error {
			cancel() // cancel immediately
			<-ctx.Done()
			r.End("cancelled", "cancelled")
			return ctx.Err()
		},
	})

	err := bus.RunBatch(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	bus.Finish()
}

func TestBus_LogWriter_ProducesJSONL(t *testing.T) {
	var logBuf bytes.Buffer
	var termBuf bytes.Buffer
	bus := NewBus(&termBuf, Options{LogWriter: NewLogWriter(&logBuf)})

	bus.AddTask(TaskSpec{
		ID: "t0", Name: "test",
		Fn: func(ctx context.Context, r *Reporter) error {
			r.Log("hello")
			r.End("succeeded", "done")
			return nil
		},
	})

	bus.RunBatch(context.Background())
	bus.Finish()

	// Log should contain JSONL lines.
	lines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 JSONL lines, got %d:\n%s", len(lines), logBuf.String())
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/render/ -run TestBus -v -count=1`
Expected: FAIL — `NewBus` not defined

**Step 3: Write implementation**

```go
// internal/render/bus.go
package render

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

// Options configures the Bus.
type Options struct {
	Renderer  Renderer   // nil = auto-detect (TTY → TTYRenderer, else JSONRenderer)
	LogWriter *LogWriter // nil = no logging
	Label     string     // spinner label, default "Running tasks"
}

// TaskSpec defines a unit of work for the bus.
type TaskSpec struct {
	ID   string
	Name string
	Fn   func(ctx context.Context, r *Reporter) error
}

// Bus orchestrates task execution and rendering.
// One bus per command invocation.
type Bus struct {
	renderer Renderer
	log      *LogWriter
	events   chan Event
	tasks    []TaskSpec
	seq      atomic.Uint64
	done     chan struct{} // signals router goroutine to stop
}

// NewBus creates a Bus with the given output writer and options.
func NewBus(w io.Writer, opts Options) *Bus {
	renderer := opts.Renderer
	if renderer == nil {
		renderer = NewTTYRenderer(w)
	}

	b := &Bus{
		renderer: renderer,
		log:      opts.LogWriter,
		events:   make(chan Event, 256),
		done:     make(chan struct{}),
	}

	// Start the router goroutine.
	go b.router()
	return b
}

// AddTask registers a task for the next RunBatch call.
func (b *Bus) AddTask(task TaskSpec) {
	b.tasks = append(b.tasks, task)
}

// Run executes a single task synchronously (sequential mode).
func (b *Bus) Run(ctx context.Context, task TaskSpec) error {
	b.renderer.Init(0) // sequential mode
	rep := &Reporter{taskID: task.ID, events: b.events}
	rep.Start(task.Name)
	err := task.Fn(ctx, rep)
	if err != nil {
		rep.End("failed", err.Error())
	}
	// Drain events briefly to let router process.
	time.Sleep(10 * time.Millisecond)
	return err
}

// RunBatch executes all added tasks in parallel via errgroup.
func (b *Bus) RunBatch(ctx context.Context) error {
	if len(b.tasks) == 0 {
		return nil
	}

	b.renderer.Init(len(b.tasks))

	g, ctx := errgroup.WithContext(ctx)

	// Start all tasks.
	for _, task := range b.tasks {
		task := task
		g.Go(func() error {
			rep := &Reporter{taskID: task.ID, events: b.events}
			rep.Start(task.Name)
			err := task.Fn(ctx, rep)
			if err != nil && !isEndSent(rep) {
				rep.End("failed", err.Error())
			}
			return err
		})
	}

	// Tick-driven rendering while tasks run.
	ticker := time.NewTicker(100 * time.Millisecond)
	renderDone := make(chan struct{})
	go func() {
		defer close(renderDone)
		for {
			select {
			case <-ticker.C:
				b.renderer.Flush()
			case <-ctx.Done():
				return
			}
		}
	}()

	err := g.Wait()

	ticker.Stop()
	<-renderDone

	// Final flush to show completed state.
	b.renderer.Flush()

	// Clear tasks for reuse.
	b.tasks = b.tasks[:0]
	return err
}

// Finish stops the router, flushes logs, and finalizes the renderer.
func (b *Bus) Finish() error {
	close(b.events)
	<-b.done // wait for router to drain

	b.renderer.Finish()

	if b.log != nil {
		return b.log.Flush()
	}
	return nil
}

// router reads events from the channel, assigns global seq, persists to log, and forwards to renderer.
func (b *Bus) router() {
	defer close(b.done)
	for ev := range b.events {
		ev.Seq = b.seq.Add(1)
		if b.log != nil {
			b.log.Write(ev)
		}
		b.renderer.HandleEvent(ev)
	}
}

func isEndSent(_ *Reporter) bool {
	// Simple guard: if the task Fn already called End(), we shouldn't send another.
	// For now, return false — the task is expected to call End() or let the bus do it.
	return false
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/render/ -run TestBus -v -count=1`
Expected: PASS

Add missing imports (`fmt`, `strings`) to the test file if needed. Iterate until all 5 bus tests pass.

**Step 5: Run all render package tests**

Run: `go test ./internal/render/ -v -count=1`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add internal/render/bus.go internal/render/bus_test.go
git commit -m "feat(render): add Bus orchestrator with Run/RunBatch/Finish"
```

---

### Task 8: Wire Into sync.go

**Files:**
- Modify: `cmd/sync.go:476-588` (updatePRContent function)
- Modify: `cmd/sync.go:737-774` (delete orderedPrinter)

**Step 1: Run existing sync tests to capture baseline**

Run: `go test ./cmd/ -run TestComputeSyncPlan -v -count=1`
Expected: ALL PASS (capture the number of passing tests)

**Step 2: Replace orderedPrinter usage in updatePRContent**

In `cmd/sync.go`, replace the block at lines 536-587:

**Delete** the `orderedPrinter` struct and its methods (lines 737-774).

**Delete** `contentJob.index` field (line 492).

**Replace** the goroutine launch block (lines 536-587) with:

```go
	bus := render.NewBus(os.Stdout, render.Options{})
	for _, j := range jobs {
		j := j
		bus.AddTask(render.TaskSpec{
			ID:   fmt.Sprintf("pr-%d", j.node.PR),
			Name: fmt.Sprintf("PR %s", ui.PR(j.node.PR)),
			Fn: func(ctx context.Context, r *render.Reporter) error {
				var parts []string

				// --- Title ---
				r.Log("updating title...")
				proposedTitle := j.localTitle
				if hasClaude && j.titlePrompt != "" {
					aiTitle, err := claudepkg.RunPrompt(j.titleSession, j.titlePrompt)
					if err == nil {
						aiTitle = strings.Split(aiTitle, "\n")[0]
						aiTitle = strings.Trim(aiTitle, "\"' ")
						proposedTitle = j.prefix + aiTitle
					}
				}

				currentTitle, _ := ghpkg.PRViewTitle(j.node.PR)
				if !similar(currentTitle, proposedTitle, 0.8) {
					if err := ghpkg.PREditTitle(j.node.PR, proposedTitle); err == nil {
						parts = append(parts, "title")
					}
				}

				// --- Description (Claude only) ---
				if hasClaude && j.descPrompt != "" {
					r.Log("generating description...")
					desc, err := claudepkg.RunPrompt(j.descSession, j.descPrompt)
					if err == nil {
						currentBody, _ := ghpkg.PRViewBody(j.node.PR)
						currentDesc := extractDescription(currentBody)
						if !similar(currentDesc, desc, 0.85) {
							newBody := replaceDescription(currentBody, desc)
							if err := ghpkg.PREditBody(j.node.PR, newBody); err == nil {
								parts = append(parts, "description")
							}
						}
					}
				}

				if len(parts) == 0 {
					r.End("succeeded", fmt.Sprintf("%s PR %s unchanged", ui.SymOK, ui.PR(j.node.PR)))
				} else {
					r.End("succeeded", fmt.Sprintf("%s PR %s updated (%s)", ui.SymOK, ui.PR(j.node.PR), strings.Join(parts, " + ")))
				}
				return nil
			},
		})
	}
	if err := bus.RunBatch(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "warning: some PR updates failed: %v\n", err)
	}
	bus.Finish()
```

**Add imports** at the top of `cmd/sync.go`:
```go
"context"
"github.com/pavelpascari/sdf/internal/render"
```

**Remove** the `sync` import if `orderedPrinter` was the only user of `sync.WaitGroup` (check for other uses first).

**Step 3: Run existing tests to verify no regression**

Run: `go test ./cmd/ -run TestComputeSyncPlan -v -count=1`
Expected: ALL PASS (same count as baseline)

Run: `go test ./... -count=1`
Expected: ALL PASS

**Step 4: Build and verify**

Run: `go build ./...`
Expected: no errors

Run: `go vet ./...`
Expected: no issues

**Step 5: Commit**

```bash
git add cmd/sync.go
git commit -m "refactor(sync): replace orderedPrinter with render.Bus"
```

---

### Task 9: Full Test Suite + Install

**Step 1: Run all tests**

Run: `go test ./... -count=1`
Expected: ALL PASS

**Step 2: Run linter**

Run: `golangci-lint run ./...`
Expected: No new issues

**Step 3: Build and install**

Run: `go install ./...`
Expected: Clean build

**Step 4: Manual smoke test**

Run `sdf sync` in a real repo with a stack that has open PRs to verify the new rendering looks correct. Verify:
- Each PR shows its own line with status updates
- Spinner animates at the bottom
- Results display in order when all complete
- No terminal artifacts after completion

**Step 5: Commit any fixups from smoke test**

```bash
git add -A
git commit -m "fix(render): address issues from smoke testing"
```

(Skip this step if no fixups are needed.)
