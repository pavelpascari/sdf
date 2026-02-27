package render

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

// Options configures a Bus.
type Options struct {
	Renderer  Renderer   // nil = default to TTYRenderer
	LogWriter *LogWriter // nil = no logging
	Label     string     // spinner label, default "Running tasks"
}

// TaskSpec describes a task to be executed by the Bus.
type TaskSpec struct {
	ID   string
	Name string
	Fn   func(ctx context.Context, r *Reporter) error
}

// Bus orchestrates task execution and rendering. It wires Reporters to an
// event channel, routes events to a Renderer (and optional LogWriter), and
// provides Run (sequential) and RunBatch (parallel) execution modes.
type Bus struct {
	renderer Renderer
	log      *LogWriter
	events   chan Event
	tasks    []TaskSpec
	seq      atomic.Uint64
	done     chan struct{} // signals router goroutine stopped
	label    string
}

// NewBus creates a Bus that writes output to w (stdout) and errw (stderr).
// If opts.Renderer is nil, a TTYRenderer is created with both writers.
// The router goroutine is started immediately.
func NewBus(w, errw io.Writer, opts Options) *Bus {
	r := opts.Renderer
	if r == nil {
		r = NewTTYRenderer(w, errw)
	}

	label := opts.Label
	if label == "" {
		label = "Running tasks"
	}

	b := &Bus{
		renderer: r,
		log:      opts.LogWriter,
		events:   make(chan Event, 256),
		done:     make(chan struct{}),
		label:    label,
	}

	go b.router()
	return b
}

// AddTask appends a task to the internal slice for the next RunBatch call.
func (b *Bus) AddTask(task TaskSpec) {
	b.tasks = append(b.tasks, task)
}

// Print sends an EventPrint event through the bus.
func (b *Bus) Print(text string) {
	b.events <- Event{
		Type: EventPrint,
		TS:   time.Now(),
		Data: map[string]any{"text": text},
	}
}

// Printf is a convenience wrapper around Print with fmt.Sprintf formatting.
func (b *Bus) Printf(format string, args ...any) {
	b.Print(fmt.Sprintf(format, args...))
}

// Warn sends an EventWarn event through the bus.
func (b *Bus) Warn(text string) {
	b.events <- Event{
		Type: EventWarn,
		TS:   time.Now(),
		Data: map[string]any{"text": text},
	}
}

// Warnf is a convenience wrapper around Warn with fmt.Sprintf formatting.
func (b *Bus) Warnf(format string, args ...any) {
	b.Warn(fmt.Sprintf(format, args...))
}

// Err sends an EventErr event through the bus.
func (b *Bus) Err(err error) {
	b.events <- Event{
		Type: EventErr,
		TS:   time.Now(),
		Data: map[string]any{"text": err.Error()},
	}
}

// Pause sends an EventPause event through the bus.
func (b *Bus) Pause() {
	b.events <- Event{
		Type: EventPause,
		TS:   time.Now(),
	}
}

// Resume sends an EventResume event through the bus.
func (b *Bus) Resume() {
	b.events <- Event{
		Type: EventResume,
		TS:   time.Now(),
	}
}

// Run executes a single task sequentially (no parallel spinner).
func (b *Bus) Run(ctx context.Context, task TaskSpec) error {
	reporter := NewReporter(task.ID, b.events)
	reporter.Start(task.Name)

	err := task.Fn(ctx, reporter)
	if err != nil {
		// Only send End if the task didn't already call it.
		// We detect this by checking if there's a pending task.end in
		// the channel — but that's unreliable. Instead, we always send
		// a "failed" end; the renderer handles duplicates gracefully.
		reporter.End("failed", err.Error())
	}

	// Give the router a moment to drain the events from this task.
	time.Sleep(10 * time.Millisecond)

	return err
}

// RunBatch executes all added tasks in parallel via errgroup.
func (b *Bus) RunBatch(ctx context.Context) error {
	if len(b.tasks) == 0 {
		return nil
	}

	tasks := b.tasks
	b.tasks = nil

	// Signal batch start to the renderer.
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

	// Signal batch end to the renderer.
	b.events <- Event{
		Type: EventBatchEnd,
		TS:   time.Now(),
	}

	// Let the router drain the batch.end event before returning.
	time.Sleep(10 * time.Millisecond)

	return err
}

// Finish closes the event channel, waits for the router to drain, calls
// renderer.Finish, and flushes the LogWriter if present.
func (b *Bus) Finish() error {
	close(b.events)
	<-b.done // wait for router to finish draining

	b.renderer.Finish()

	if b.log != nil {
		return b.log.Flush()
	}
	return nil
}

// router reads events from the channel, assigns sequence numbers, and
// dispatches to the LogWriter and Renderer. It runs until the events
// channel is closed.
func (b *Bus) router() {
	defer close(b.done)
	for ev := range b.events {
		ev.Seq = b.seq.Add(1)
		if b.log != nil {
			_ = b.log.Write(ev)
		}
		b.renderer.HandleEvent(ev)
	}
}
