package render

import (
	"fmt"
	"io"
	"sync"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type slot struct {
	label  string // task name from task.start
	status string // current status from task.log, overwritten each time
	done   bool
	final  string // final line from task.end message
}

// TTYRenderer renders task progress to a terminal using ANSI escape sequences.
// It supports two modes: batch mode (multi-line in-place updates with a spinner)
// and sequential mode (append-only output).
type TTYRenderer struct {
	mu           sync.Mutex
	w            io.Writer
	errw         io.Writer
	slots        []slot
	taskIndex    map[string]int // taskID → slot index
	completed    int
	spinnerFrame int
	batchMode    bool
	paused       bool
	label        string // spinner label, default "Running tasks"
}

// NewTTYRenderer creates a TTYRenderer that writes normal output to w and
// warnings/errors to errw.
func NewTTYRenderer(w, errw io.Writer) *TTYRenderer {
	return &TTYRenderer{
		w:         w,
		errw:      errw,
		taskIndex: make(map[string]int),
		label:     "Running tasks",
	}
}

// HandleEvent processes a render event. In batch mode, events update slot state
// which is rendered on the next Flush. In sequential mode, log and end events
// are printed immediately.
func (r *TTYRenderer) HandleEvent(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch e.Type {
	case EventTaskStart:
		r.handleTaskStart(e)
	case EventTaskLog:
		r.handleTaskLog(e)
	case EventTaskEnd:
		r.handleTaskEnd(e)
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
	}
}

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

	// Read count — handle both int and float64 (JSON unmarshalled).
	var count int
	switch v := data["count"].(type) {
	case int:
		count = v
	case float64:
		count = int(v)
	}

	if label, ok := data["label"].(string); ok && label != "" {
		r.label = label
	}

	if count > 0 {
		r.batchMode = true
		r.slots = make([]slot, 0, count)
		// Reserve n+2 lines (n slots + blank line + spinner line).
		for i := 0; i < count+2; i++ {
			fmt.Fprintln(r.w)
		}
		fmt.Fprintf(r.w, "%s", HideCursor())
	}
}

func (r *TTYRenderer) handleBatchEnd() {
	if !r.batchMode {
		return
	}

	// Final flush to render the last state.
	r.paused = false
	r.flushLocked()
	fmt.Fprintf(r.w, "%s", ShowCursor())

	// Reset batch state.
	r.batchMode = false
	r.slots = nil
	r.taskIndex = make(map[string]int)
	r.completed = 0
	r.spinnerFrame = 0
}

func (r *TTYRenderer) handleTaskStart(e Event) {
	data, ok := e.Data.(map[string]any)
	if !ok {
		return
	}
	name, _ := data["name"].(string)

	idx := len(r.slots)
	r.slots = append(r.slots, slot{label: name, status: "waiting..."})
	r.taskIndex[e.TaskID] = idx
}

func (r *TTYRenderer) handleTaskLog(e Event) {
	data, ok := e.Data.(map[string]any)
	if !ok {
		return
	}
	text, _ := data["text"].(string)

	if !r.batchMode {
		fmt.Fprintf(r.w, "  %s\n", text)
		return
	}

	idx, ok := r.taskIndex[e.TaskID]
	if !ok {
		return
	}
	r.slots[idx].status = text
}

func (r *TTYRenderer) handleTaskEnd(e Event) {
	data, ok := e.Data.(map[string]any)
	if !ok {
		return
	}
	message, _ := data["message"].(string)

	if !r.batchMode {
		fmt.Fprintf(r.w, "  %s\n", message)
		return
	}

	idx, ok := r.taskIndex[e.TaskID]
	if !ok {
		return
	}
	r.slots[idx].done = true
	r.slots[idx].final = message
	r.completed++
}

// Flush rewrites the in-place block in batch mode. In sequential mode this is a no-op.
// If the renderer is paused, Flush is a no-op to avoid interfering with interactive prompts.
func (r *TTYRenderer) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.batchMode || r.paused {
		return
	}

	r.flushLocked()
}

func (r *TTYRenderer) flushLocked() {
	totalLines := len(r.slots) + 2 // slots + blank line + spinner line

	// Move cursor back to the top of the block.
	fmt.Fprintf(r.w, "\r%s", CursorUp(totalLines))

	// Write each slot line.
	for i := range r.slots {
		if r.slots[i].done {
			fmt.Fprintf(r.w, "%s  %s\n", El(), r.slots[i].final)
		} else {
			fmt.Fprintf(r.w, "%s  %s: %s\n", El(), r.slots[i].label, r.slots[i].status)
		}
	}

	// Blank separator line.
	fmt.Fprintf(r.w, "%s\n", El())

	// Spinner line with completion counter.
	frame := spinnerFrames[r.spinnerFrame%len(spinnerFrames)]
	fmt.Fprintf(r.w, "%s  %s %s (%d/%d)\n", El(), frame, r.label, r.completed, len(r.slots))

	r.spinnerFrame++
}

// Finish ensures the cursor is shown. Batch cleanup is handled by
// EventBatchEnd, so this is a safety net.
func (r *TTYRenderer) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.batchMode {
		r.paused = false
		r.flushLocked()
		fmt.Fprintf(r.w, "%s", ShowCursor())
	}
}
