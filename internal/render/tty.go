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
	slots        []slot
	taskIndex    map[string]int // taskID → slot index
	completed    int
	spinnerFrame int
	batchMode    bool
	paused       bool
	label        string // spinner label, default "Running tasks"
}

// NewTTYRenderer creates a TTYRenderer that writes to w.
func NewTTYRenderer(w io.Writer) *TTYRenderer {
	return &TTYRenderer{
		w:         w,
		taskIndex: make(map[string]int),
		label:     "Running tasks",
	}
}

// SetLabel overrides the default spinner label ("Running tasks").
func (r *TTYRenderer) SetLabel(label string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.label = label
}

// Init sets up the renderer. If taskCount > 0, batch mode is activated:
// n slots are allocated and n+2 blank lines are printed to reserve screen space.
// If taskCount is 0, sequential mode is used (append-only, no cursor movement).
func (r *TTYRenderer) Init(taskCount int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if taskCount > 0 {
		r.batchMode = true
		r.slots = make([]slot, 0, taskCount)
		// Reserve n+2 lines (n slots + blank line + spinner line).
		for i := 0; i < taskCount+2; i++ {
			fmt.Fprintln(r.w)
		}
		fmt.Fprintf(r.w, "%s", HideCursor())
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
	case EventPause:
		r.paused = true
	case EventResume:
		r.paused = false
	}
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

// Pause sets the renderer to paused state and shows the cursor so interactive
// prompts can work correctly.
func (r *TTYRenderer) Pause() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.paused = true
	if r.batchMode {
		fmt.Fprintf(r.w, "%s", ShowCursor())
	}
}

// Resume clears the paused state and hides the cursor for batch mode rendering.
func (r *TTYRenderer) Resume() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.paused = false
	if r.batchMode {
		fmt.Fprintf(r.w, "%s", HideCursor())
	}
}

// Finish performs a final Flush and restores the cursor.
func (r *TTYRenderer) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.batchMode {
		r.paused = false
		r.flushLocked()
		fmt.Fprintf(r.w, "%s", ShowCursor())
	}
}
