# Bus Evolution Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend the render.Bus with Print/Warn/Err methods and event-driven batch lifecycle so commands never call fmt.Print directly.

**Architecture:** Add new event types (bus.print, bus.warn, bus.err, batch.start, batch.end), simplify the Renderer interface to HandleEvent/Flush/Finish, update TTYRenderer to handle two writers (stdout + stderr) and mode transitions via events, update JSONRenderer to collect warnings and errors.

**Tech Stack:** Go 1.24, golang.org/x/sync/errgroup

**Design doc:** `docs/plans/2026-02-28-bus-evolution-design.md`

---

### Task 1: Add new event type constants

**Files:**
- Modify: `internal/render/event.go`
- Modify: `internal/render/event_test.go`

**Step 1: Add test cases for new constants**

Add these cases to the existing `TestEventConstants` table in `event_test.go`:

```go
{"EventPrint", EventPrint, "bus.print"},
{"EventWarn", EventWarn, "bus.warn"},
{"EventErr", EventErr, "bus.err"},
{"EventBatchStart", EventBatchStart, "batch.start"},
{"EventBatchEnd", EventBatchEnd, "batch.end"},
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/render/ -run TestEventConstants -v`
Expected: FAIL — `EventPrint` undefined

**Step 3: Add the constants to event.go**

Add to the existing `const` block in `event.go`:

```go
// Bus-level output events.
EventPrint = "bus.print"
EventWarn  = "bus.warn"
EventErr   = "bus.err"

// Batch lifecycle events.
EventBatchStart = "batch.start"
EventBatchEnd   = "batch.end"
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/render/ -run TestEventConstants -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/render/event.go internal/render/event_test.go
git commit -m "feat(render): add bus output and batch lifecycle event constants"
```

---

### Task 2: Simplify Renderer interface

Remove `Init`, `Pause`, `Resume` from the Renderer interface. These become event-driven.

**Files:**
- Modify: `internal/render/renderer.go`
- Modify: `internal/render/json.go`
- Modify: `internal/render/json_test.go`
- Modify: `internal/render/tty.go`
- Modify: `internal/render/tty_test.go`
- Modify: `internal/render/bus.go`

**Step 1: Update the Renderer interface**

Replace the entire `renderer.go` with:

```go
package render

// Renderer defines a pluggable output backend for the event bus.
type Renderer interface {
	HandleEvent(Event)
	Flush()
	Finish()
}
```

**Step 2: Remove Init/Pause/Resume methods from JSONRenderer**

In `json.go`, remove these methods:
- `func (r *JSONRenderer) Init(taskCount int)` — remove entirely
- `func (r *JSONRenderer) Pause()` — remove entirely
- `func (r *JSONRenderer) Resume()` — remove entirely

The `results` slice will be lazily initialized (append to nil is safe in Go).

**Step 3: Update JSONRenderer tests**

In `json_test.go`:
- Remove all `r.Init(N)` calls — they're no longer needed
- Remove `TestJSONRendererNoOpsDoNotPanic` test — `Pause`/`Resume` no longer exist on the interface
- Replace it with a simpler no-op test:

```go
func TestJSONRendererNoOpsDoNotPanic(t *testing.T) {
	r := &JSONRenderer{}
	r.Flush()
	r.Finish()
}
```

**Step 4: Remove Init/Pause/Resume from TTYRenderer's public API**

In `tty.go`:
- Remove the `Init` method entirely (its logic moves to `handleBatchStart` in Task 4)
- Remove the `Pause()` method (becomes `handlePause` in `HandleEvent`)
- Remove the `Resume()` method (becomes `handleResume` in `HandleEvent`)

The `HandleEvent` switch already has cases for `EventPause`/`EventResume` that set `r.paused` — keep that logic, just inline the cursor show/hide:

```go
case EventPause:
	r.paused = true
	if r.batchMode {
		fmt.Fprintf(r.w, "%s", ShowCursor())
	}
case EventResume:
	r.paused = false
	if r.batchMode {
		fmt.Fprintf(r.w, "%s", HideCursor())
	}
```

**Step 5: Update TTYRenderer tests**

In `tty_test.go`:
- Replace all `r.Init(N)` calls with direct `batch.start` events (this will compile but the handler doesn't exist yet — add a TODO or skip for now)
- Actually: **leave tests temporarily broken** — Task 4 adds `handleBatchStart` which makes them pass again. Better: update tests to send `batch.start` events and simultaneously add the handler.

The safest approach: do this task and Task 4 together. But for TDD purity, update the tests to use `batch.start` events now, verify they fail (no handler), then Task 4 adds the handler.

Replace `r.Init(2)` calls with:

```go
r.HandleEvent(Event{
	Type: EventBatchStart,
	Data: map[string]any{"count": 2, "label": "Running tasks"},
})
```

Replace `r.Init(0)` (sequential mode) by simply not sending a `batch.start` — sequential is the default.

Replace `r.Pause()` calls with:

```go
r.HandleEvent(Event{Type: EventPause})
```

**Step 6: Update bus.go**

Remove the `b.renderer.Init(0)` call from `Run()` and the `b.renderer.Init(len(tasks))` call from `RunBatch()`. These will be replaced with batch events in Task 5.

**Step 7: Verify the build compiles**

Run: `go build ./...`
Expected: PASS (compiles). Tests may fail because batch mode isn't triggered yet — that's expected until Task 4.

**Step 8: Run tests**

Run: `go test ./internal/render/ -v`
Expected: Some TTY tests may fail (no batch mode activation). That's OK — Task 4 fixes them.

**Step 9: Commit**

```bash
git add internal/render/renderer.go internal/render/json.go internal/render/json_test.go internal/render/tty.go internal/render/tty_test.go internal/render/bus.go
git commit -m "refactor(render): simplify Renderer interface to HandleEvent/Flush/Finish"
```

---

### Task 3: Add Print/Warn/Err to Bus

**Files:**
- Modify: `internal/render/bus.go`
- Modify: `internal/render/bus_test.go`

**Step 1: Write failing tests**

Add to `bus_test.go`:

```go
func TestBus_Print_EmitsEvent(t *testing.T) {
	var buf bytes.Buffer
	var logBuf bytes.Buffer
	lw := NewLogWriter(&logBuf)
	bus := NewBus(&buf, &buf, Options{LogWriter: lw})

	bus.Print("hello world")
	if err := bus.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	// Verify JSONL log captured the print event.
	raw := logBuf.String()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 log line, got %d: %q", len(lines), raw)
	}
	var ev Event
	if err := json.Unmarshal([]byte(lines[0]), &ev); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if ev.Type != EventPrint {
		t.Errorf("Type: got %q, want %q", ev.Type, EventPrint)
	}
	if ev.TaskID != "" {
		t.Errorf("TaskID: got %q, want empty", ev.TaskID)
	}
}

func TestBus_Printf_FormatsText(t *testing.T) {
	var buf bytes.Buffer
	var logBuf bytes.Buffer
	lw := NewLogWriter(&logBuf)
	bus := NewBus(&buf, &buf, Options{LogWriter: lw})

	bus.Printf("count: %d", 42)
	if err := bus.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	raw := logBuf.String()
	if !strings.Contains(raw, "count: 42") {
		t.Errorf("expected formatted text in log, got: %s", raw)
	}
}

func TestBus_Warn_EmitsEvent(t *testing.T) {
	var buf bytes.Buffer
	var logBuf bytes.Buffer
	lw := NewLogWriter(&logBuf)
	bus := NewBus(&buf, &buf, Options{LogWriter: lw})

	bus.Warn("something is off")
	if err := bus.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	raw := logBuf.String()
	var ev Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &ev); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if ev.Type != EventWarn {
		t.Errorf("Type: got %q, want %q", ev.Type, EventWarn)
	}
}

func TestBus_Err_EmitsEvent(t *testing.T) {
	var buf bytes.Buffer
	var logBuf bytes.Buffer
	lw := NewLogWriter(&logBuf)
	bus := NewBus(&buf, &buf, Options{LogWriter: lw})

	bus.Err(errors.New("boom"))
	if err := bus.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	raw := logBuf.String()
	var ev Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &ev); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if ev.Type != EventErr {
		t.Errorf("Type: got %q, want %q", ev.Type, EventErr)
	}
	data, _ := ev.Data.(map[string]any)
	if data["text"] != "boom" {
		t.Errorf("Data[text]: got %v, want %q", data["text"], "boom")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/render/ -run TestBus_Print -v`
Expected: FAIL — `NewBus` has wrong signature, `Print`/`Printf`/`Warn`/`Err` undefined

**Step 3: Update Bus constructor and add methods**

In `bus.go`, update `NewBus` signature to accept both writers:

```go
func NewBus(w, errw io.Writer, opts Options) *Bus {
```

Add `errw` field to the Bus struct (for passing to default TTYRenderer).

Update the default TTYRenderer creation — for now just pass `w` (Task 4 updates TTYRenderer to accept both). The `errw` will be stored but not used until TTYRenderer is updated.

Add the new methods:

```go
func (b *Bus) Print(text string) {
	b.events <- Event{
		Type: EventPrint,
		TS:   time.Now(),
		Data: map[string]any{"text": text},
	}
}

func (b *Bus) Printf(format string, args ...any) {
	b.Print(fmt.Sprintf(format, args...))
}

func (b *Bus) Warn(text string) {
	b.events <- Event{
		Type: EventWarn,
		TS:   time.Now(),
		Data: map[string]any{"text": text},
	}
}

func (b *Bus) Warnf(format string, args ...any) {
	b.Warn(fmt.Sprintf(format, args...))
}

func (b *Bus) Err(err error) {
	b.events <- Event{
		Type: EventErr,
		TS:   time.Now(),
		Data: map[string]any{"text": err.Error()},
	}
}

func (b *Bus) Pause() {
	b.events <- Event{
		Type: EventPause,
		TS:   time.Now(),
	}
}

func (b *Bus) Resume() {
	b.events <- Event{
		Type: EventResume,
		TS:   time.Now(),
	}
}
```

**Step 4: Fix existing test call sites**

All existing tests call `NewBus(&buf, Options{...})` — update them to `NewBus(&buf, &buf, Options{...})` (use same buffer for both writers in tests, since we're not testing stderr separation yet).

Update these tests in `bus_test.go`:
- `TestBus_RunBatch_ExecutesAllTasks`
- `TestBus_RunBatch_PropagatesError`
- `TestBus_Run_ExecutesSingleTask`
- `TestBus_RunBatch_CancelsOnContext`
- `TestBus_LogWriter_ProducesJSONL`

**Step 5: Run tests**

Run: `go test ./internal/render/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/render/bus.go internal/render/bus_test.go
git commit -m "feat(render): add Print/Printf/Warn/Warnf/Err/Pause/Resume to Bus"
```

---

### Task 4: Update TTYRenderer for event-driven mode transitions and dual writers

**Files:**
- Modify: `internal/render/tty.go`
- Modify: `internal/render/tty_test.go`

**Step 1: Write failing tests for new event handling**

Add to `tty_test.go`:

```go
func TestTTYRenderer_Print_WritesToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := NewTTYRenderer(&stdout, &stderr)

	r.HandleEvent(Event{
		Type: EventPrint,
		Data: map[string]any{"text": "hello world"},
	})

	if !strings.Contains(stdout.String(), "hello world") {
		t.Errorf("expected stdout to contain %q, got: %q", "hello world", stdout.String())
	}
	if stderr.Len() > 0 {
		t.Errorf("expected empty stderr, got: %q", stderr.String())
	}
}

func TestTTYRenderer_Warn_WritesToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := NewTTYRenderer(&stdout, &stderr)

	r.HandleEvent(Event{
		Type: EventWarn,
		Data: map[string]any{"text": "watch out"},
	})

	if !strings.Contains(stderr.String(), "watch out") {
		t.Errorf("expected stderr to contain %q, got: %q", "watch out", stderr.String())
	}
	if stdout.Len() > 0 {
		t.Errorf("expected empty stdout, got: %q", stdout.String())
	}
}

func TestTTYRenderer_Err_WritesToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := NewTTYRenderer(&stdout, &stderr)

	r.HandleEvent(Event{
		Type: EventErr,
		Data: map[string]any{"text": "something broke"},
	})

	if !strings.Contains(stderr.String(), "something broke") {
		t.Errorf("expected stderr to contain %q, got: %q", "something broke", stderr.String())
	}
}

func TestTTYRenderer_BatchStartEntersBatchMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := NewTTYRenderer(&stdout, &stderr)

	r.HandleEvent(Event{
		Type: EventBatchStart,
		Data: map[string]any{"count": 2, "label": "Test batch"},
	})

	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TaskID: "t1",
		Data:   map[string]any{"name": "Task One"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TaskID: "t2",
		Data:   map[string]any{"name": "Task Two"},
	})

	r.Flush()
	out := stdout.String()

	if !strings.Contains(out, "Task One") {
		t.Errorf("expected output to contain %q, got:\n%s", "Task One", out)
	}
	if !strings.Contains(out, "Test batch") {
		t.Errorf("expected output to contain label %q, got:\n%s", "Test batch", out)
	}
}

func TestTTYRenderer_BatchEndExitsBatchMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := NewTTYRenderer(&stdout, &stderr)

	r.HandleEvent(Event{
		Type: EventBatchStart,
		Data: map[string]any{"count": 1, "label": "Batch"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskStart,
		TaskID: "t1",
		Data:   map[string]any{"name": "Only task"},
	})
	r.HandleEvent(Event{
		Type:   EventTaskEnd,
		TaskID: "t1",
		Data:   map[string]any{"status": "ok", "message": "done"},
	})
	r.HandleEvent(Event{Type: EventBatchEnd})

	// After batch.end, a bus.print should go directly to stdout (not into batch slots).
	stdout.Reset()
	stderr.Reset()

	r.HandleEvent(Event{
		Type: EventPrint,
		Data: map[string]any{"text": "summary line"},
	})

	if !strings.Contains(stdout.String(), "summary line") {
		t.Errorf("expected post-batch print, got: %q", stdout.String())
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/render/ -run TestTTYRenderer_Print -v`
Expected: FAIL — `NewTTYRenderer` has wrong signature

**Step 3: Update TTYRenderer**

In `tty.go`:

1. Add `errw io.Writer` field to the struct.

2. Update constructor:
```go
func NewTTYRenderer(w, errw io.Writer) *TTYRenderer {
	return &TTYRenderer{
		w:         w,
		errw:      errw,
		taskIndex: make(map[string]int),
		label:     "Running tasks",
	}
}
```

3. Add handlers in the `HandleEvent` switch:

```go
case EventPrint:
	r.handlePrint(e)
case EventWarn:
	r.handleWarn(e)
case EventErr:
	r.handleErr(e)
case EventBatchStart:
	r.handleBatchStart(e)
case EventBatchEnd:
	r.handleBatchEnd()
```

4. Implement new handlers:

```go
func (r *TTYRenderer) handlePrint(e Event) {
	data, ok := e.Data.(map[string]any)
	if !ok {
		return
	}
	text, _ := data["text"].(string)
	fmt.Fprintf(r.w, "%s\n", text)
}

func (r *TTYRenderer) handleWarn(e Event) {
	data, ok := e.Data.(map[string]any)
	if !ok {
		return
	}
	text, _ := data["text"].(string)
	fmt.Fprintf(r.errw, "%s\n", text)
}

func (r *TTYRenderer) handleErr(e Event) {
	data, ok := e.Data.(map[string]any)
	if !ok {
		return
	}
	text, _ := data["text"].(string)
	fmt.Fprintf(r.errw, "%s\n", text)
}

func (r *TTYRenderer) handleBatchStart(e Event) {
	data, ok := e.Data.(map[string]any)
	if !ok {
		return
	}
	count, _ := data["count"].(int)
	if label, ok := data["label"].(string); ok && label != "" {
		r.label = label
	}

	r.batchMode = true
	r.slots = make([]slot, 0, count)
	r.completed = 0
	r.spinnerFrame = 0
	// Reserve n+2 lines (n slots + blank line + spinner line).
	for i := 0; i < count+2; i++ {
		fmt.Fprintln(r.w)
	}
	fmt.Fprintf(r.w, "%s", HideCursor())
}

func (r *TTYRenderer) handleBatchEnd() {
	if !r.batchMode {
		return
	}
	r.paused = false
	r.flushLocked()
	fmt.Fprintf(r.w, "%s", ShowCursor())
	r.batchMode = false
	// Reset batch state for potential next batch.
	r.slots = nil
	r.taskIndex = make(map[string]int)
	r.completed = 0
}
```

**NOTE on `handleBatchStart` count type:** When the event goes through the channel and gets deserialized, `int` values in `map[string]any` may arrive as `float64` from JSON. But since these events are created in-process (not deserialized), they'll be `int`. However, be safe and handle both:

```go
var count int
switch v := data["count"].(type) {
case int:
	count = v
case float64:
	count = int(v)
}
```

**Step 4: Update existing TTYRenderer tests**

All tests that call `NewTTYRenderer(&buf)` must become `NewTTYRenderer(&buf, &errBuf)` (or `NewTTYRenderer(&buf, io.Discard)` when stderr isn't being tested).

All tests that call `r.Init(N)` must send `batch.start` events instead. All tests that call `r.Pause()` must send `EventPause` events instead.

Update `TestTTYRenderer_BatchMode_ShowsSlots`:
```go
func TestTTYRenderer_BatchMode_ShowsSlots(t *testing.T) {
	var buf bytes.Buffer
	r := NewTTYRenderer(&buf, io.Discard)

	r.HandleEvent(Event{
		Type: EventBatchStart,
		Data: map[string]any{"count": 2, "label": "Running tasks"},
	})
	buf.Reset() // clear the batch.start output so we only inspect Flush output

	// ... rest unchanged ...
}
```

Apply the same pattern to:
- `TestTTYRenderer_BatchMode_EndFinalizes`
- `TestTTYRenderer_BatchMode_SpinnerCounter`
- `TestTTYRenderer_SequentialMode_AppendsLines` — remove the `r.Init(0)` call entirely (sequential is the default)
- `TestTTYRenderer_PauseShowsCursor` — replace `r.Pause()` with `r.HandleEvent(Event{Type: EventPause})`
- `TestTTYRenderer_ImplementsRenderer` — keep as-is (still checks the interface)

**Step 5: Update Bus default renderer creation**

In `bus.go`, update `NewBus` to pass both writers to TTYRenderer:

```go
func NewBus(w, errw io.Writer, opts Options) *Bus {
	r := opts.Renderer
	if r == nil {
		tty := NewTTYRenderer(w, errw)
		if opts.Label != "" {
			tty.SetLabel(opts.Label)
		}
		r = tty
	}
	// ...
}
```

**Step 6: Run all tests**

Run: `go test ./internal/render/ -v`
Expected: PASS

Run: `go build ./...`
Expected: PASS (cmd/sync.go and cmd/prnav.go still use old NewBus signature — will fail). Fix call sites in sync.go and prnav.go to pass `os.Stderr` as second arg:

In `cmd/sync.go`: `render.NewBus(os.Stdout, os.Stderr, render.Options{...})`
In `cmd/prnav.go`: `render.NewBus(os.Stdout, os.Stderr, render.Options{...})`

Run: `go build ./...`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/render/tty.go internal/render/tty_test.go internal/render/bus.go cmd/sync.go cmd/prnav.go
git commit -m "feat(render): event-driven TTYRenderer with dual writers and batch lifecycle"
```

---

### Task 5: Update Bus.Run and Bus.RunBatch to emit batch events

**Files:**
- Modify: `internal/render/bus.go`
- Modify: `internal/render/bus_test.go`

**Step 1: Write test for interleaved Run then RunBatch**

```go
func TestBus_InterleavedRunAndRunBatch(t *testing.T) {
	var stdout bytes.Buffer
	bus := NewBus(&stdout, io.Discard, Options{})

	// Sequential task
	bus.Print("starting")
	err := bus.Run(context.Background(), TaskSpec{
		ID: "seq", Name: "sequential",
		Fn: func(ctx context.Context, r *Reporter) error {
			r.Log("working")
			r.End("ok", "done")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Parallel batch
	bus.AddTask(TaskSpec{
		ID: "p1", Name: "parallel-1",
		Fn: func(ctx context.Context, r *Reporter) error {
			r.End("ok", "p1 done")
			return nil
		},
	})
	bus.AddTask(TaskSpec{
		ID: "p2", Name: "parallel-2",
		Fn: func(ctx context.Context, r *Reporter) error {
			r.End("ok", "p2 done")
			return nil
		},
	})
	if err := bus.RunBatch(context.Background()); err != nil {
		t.Fatalf("RunBatch: %v", err)
	}

	bus.Print("finished")
	if err := bus.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "starting") {
		t.Errorf("missing 'starting' in output: %s", out)
	}
	if !strings.Contains(out, "finished") {
		t.Errorf("missing 'finished' in output: %s", out)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/render/ -run TestBus_Interleaved -v`
Expected: May pass already or fail depending on current Run/RunBatch state.

**Step 3: Update RunBatch to emit batch events**

In `bus.go`, update `RunBatch`:

```go
func (b *Bus) RunBatch(ctx context.Context) error {
	if len(b.tasks) == 0 {
		return nil
	}

	tasks := b.tasks
	b.tasks = nil

	// Signal batch start to renderer.
	b.events <- Event{
		Type: EventBatchStart,
		TS:   time.Now(),
		Data: map[string]any{"count": len(tasks), "label": b.label},
	}

	g, ctx := errgroup.WithContext(ctx)

	for _, task := range tasks {
		g.Go(func() error {
			reporter := NewReporter(task.ID, b.events)
			reporter.Start(task.Name)

			err := task.Fn(ctx, reporter)
			if err != nil {
				reporter.End("failed", err.Error())
			}
			return err
		})
	}

	// Start a ticker goroutine that flushes the renderer periodically.
	tickDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				b.renderer.Flush()
			case <-tickDone:
				return
			}
		}
	}()

	err := g.Wait()

	close(tickDone)

	// Signal batch end to renderer.
	b.events <- Event{
		Type: EventBatchEnd,
		TS:   time.Now(),
	}

	// Give the router a moment to drain the batch.end event.
	time.Sleep(10 * time.Millisecond)

	return err
}
```

Add a `label` field to the Bus struct so `RunBatch` can include it in the batch.start event:

```go
type Bus struct {
	renderer Renderer
	log      *LogWriter
	events   chan Event
	tasks    []TaskSpec
	seq      atomic.Uint64
	done     chan struct{}
	label    string
}
```

Set it in `NewBus`:
```go
b := &Bus{
	renderer: r,
	log:      opts.LogWriter,
	events:   make(chan Event, 256),
	done:     make(chan struct{}),
	label:    opts.Label,
}
```

**Step 4: Update Run to not call Init**

`Run` should just send task events directly. The renderer handles them in sequential mode (the default when not in a batch):

```go
func (b *Bus) Run(ctx context.Context, task TaskSpec) error {
	reporter := NewReporter(task.ID, b.events)
	reporter.Start(task.Name)

	err := task.Fn(ctx, reporter)
	if err != nil {
		reporter.End("failed", err.Error())
	}

	// Give the router a moment to drain the events from this task.
	time.Sleep(10 * time.Millisecond)

	return err
}
```

**Step 5: Remove Finish's call to renderer.Flush()**

`Finish` currently calls `b.renderer.Finish()`. That's still correct. But verify that `Finish` on the TTYRenderer now handles the case where no batch was ever started (it should be a no-op if `batchMode` is false).

**Step 6: Run all tests**

Run: `go test ./internal/render/ -v`
Expected: PASS

Run: `go test ./... -count=1`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/render/bus.go internal/render/bus_test.go
git commit -m "feat(render): emit batch.start/batch.end events from RunBatch"
```

---

### Task 6: Update JSONRenderer to collect warnings and errors

**Files:**
- Modify: `internal/render/json.go`
- Modify: `internal/render/json_test.go`

**Step 1: Write failing tests**

Add to `json_test.go`:

```go
func TestJSONRendererCollectsWarnings(t *testing.T) {
	r := &JSONRenderer{}

	r.HandleEvent(Event{
		Type: EventWarn,
		Data: map[string]any{"text": "something is off"},
	})
	r.HandleEvent(Event{
		Type: EventWarn,
		Data: map[string]any{"text": "another warning"},
	})

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

	r.HandleEvent(Event{
		Type: EventErr,
		Data: map[string]any{"text": "something broke"},
	})

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

	r.HandleEvent(Event{
		Type: EventPrint,
		Data: map[string]any{"text": "hello"},
	})

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
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/render/ -run TestJSONRendererCollects -v`
Expected: FAIL — `Warnings()` and `Errors()` undefined

**Step 3: Update JSONRenderer**

In `json.go`, add fields and methods:

```go
type JSONRenderer struct {
	results  []TaskResult
	warnings []string
	errors   []string
}
```

Update `HandleEvent` to handle new event types:

```go
func (r *JSONRenderer) HandleEvent(e Event) {
	data, ok := e.Data.(map[string]any)
	if !ok {
		return
	}

	switch e.Type {
	case EventTaskEnd:
		status, _ := data["status"].(string)
		message, _ := data["message"].(string)
		r.results = append(r.results, TaskResult{
			TaskID:  e.TaskID,
			Status:  status,
			Message: message,
		})
	case EventWarn:
		text, _ := data["text"].(string)
		r.warnings = append(r.warnings, text)
	case EventErr:
		text, _ := data["text"].(string)
		r.errors = append(r.errors, text)
	}
}
```

Add accessors:

```go
func (r *JSONRenderer) Warnings() []string {
	return r.warnings
}

func (r *JSONRenderer) Errors() []string {
	return r.errors
}
```

**Step 4: Run tests**

Run: `go test ./internal/render/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/render/json.go internal/render/json_test.go
git commit -m "feat(render): JSONRenderer collects warnings and errors"
```

---

### Task 7: Integration test — full interleaved lifecycle

**Files:**
- Modify: `internal/render/bus_test.go`

**Step 1: Write integration test**

```go
func TestBus_FullLifecycle_InterleavedOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	bus := NewBus(&stdout, &stderr, Options{})

	// Print before any tasks.
	bus.Print("Starting operation...")

	// Sequential task.
	err := bus.Run(context.Background(), TaskSpec{
		ID: "fetch", Name: "fetch",
		Fn: func(ctx context.Context, r *Reporter) error {
			r.Log("fetching from origin")
			r.End("ok", "fetched")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Warning.
	bus.Warn("something went sideways")

	// Parallel batch.
	for i := range 2 {
		id := fmt.Sprintf("task-%d", i)
		bus.AddTask(TaskSpec{
			ID: id, Name: id,
			Fn: func(ctx context.Context, r *Reporter) error {
				r.End("ok", "done")
				return nil
			},
		})
	}
	if err := bus.RunBatch(context.Background()); err != nil {
		t.Fatalf("RunBatch: %v", err)
	}

	// Print after batch.
	bus.Print("All done.")

	if err := bus.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	out := stdout.String()
	errOut := stderr.String()

	// Verify stdout contains print output.
	if !strings.Contains(out, "Starting operation...") {
		t.Errorf("stdout missing 'Starting operation...': %s", out)
	}
	if !strings.Contains(out, "All done.") {
		t.Errorf("stdout missing 'All done.': %s", out)
	}

	// Verify stderr contains warning.
	if !strings.Contains(errOut, "something went sideways") {
		t.Errorf("stderr missing warning: %s", errOut)
	}
}

func TestBus_FullLifecycle_JSONRenderer(t *testing.T) {
	jr := &JSONRenderer{}
	bus := NewBus(io.Discard, io.Discard, Options{Renderer: jr})

	bus.Print("ignored by JSON")
	bus.Warn("collected warning")
	bus.Err(errors.New("collected error"))

	bus.AddTask(TaskSpec{
		ID: "t1", Name: "task",
		Fn: func(ctx context.Context, r *Reporter) error {
			r.End("ok", "completed")
			return nil
		},
	})
	if err := bus.RunBatch(context.Background()); err != nil {
		t.Fatalf("RunBatch: %v", err)
	}

	if err := bus.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	if len(jr.Results()) != 1 {
		t.Errorf("expected 1 result, got %d", len(jr.Results()))
	}
	if len(jr.Warnings()) != 1 {
		t.Errorf("expected 1 warning, got %d", len(jr.Warnings()))
	}
	if len(jr.Errors()) != 1 {
		t.Errorf("expected 1 error, got %d", len(jr.Errors()))
	}
}
```

**Step 2: Run tests**

Run: `go test ./internal/render/ -run TestBus_FullLifecycle -v`
Expected: PASS

**Step 3: Run full test suite**

Run: `go test ./... -count=1`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add internal/render/bus_test.go
git commit -m "test(render): add full lifecycle integration tests for bus evolution"
```

---

### Summary

| Task | What | Files |
|------|------|-------|
| 1 | New event constants | event.go, event_test.go |
| 2 | Simplify Renderer interface | renderer.go, json.go, json_test.go, tty.go, tty_test.go, bus.go |
| 3 | Add Print/Warn/Err to Bus | bus.go, bus_test.go |
| 4 | TTYRenderer dual writers + batch events | tty.go, tty_test.go, bus.go, sync.go, prnav.go |
| 5 | RunBatch emits batch events | bus.go, bus_test.go |
| 6 | JSONRenderer collects warnings/errors | json.go, json_test.go |
| 7 | Integration tests | bus_test.go |

**Note:** Tasks 2-5 are tightly coupled (interface change ripples). Consider batching 2+3+4+5 into a single commit if intermediate states don't compile cleanly. The plan orders them for logical progression, but the implementer may need to apply 2-4 atomically.
