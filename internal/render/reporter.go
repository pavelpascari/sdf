package render

import "time"

// Reporter sends events for a single task into the event channel.
type Reporter struct {
	taskID string
	events chan<- Event
}

// NewReporter creates a Reporter bound to the given task ID and event channel.
func NewReporter(taskID string, events chan<- Event) *Reporter {
	return &Reporter{taskID: taskID, events: events}
}

// Start sends an EventTaskStart event with the given task name.
func (r *Reporter) Start(name string) {
	r.events <- Event{
		Type:   EventTaskStart,
		TS:     time.Now(),
		TaskID: r.taskID,
		Data:   map[string]any{"name": name},
	}
}

// Log sends an EventTaskLog event with the given text.
func (r *Reporter) Log(text string) {
	r.events <- Event{
		Type:   EventTaskLog,
		TS:     time.Now(),
		TaskID: r.taskID,
		Data:   map[string]any{"text": text},
	}
}

// End sends an EventTaskEnd event with the given status and message.
func (r *Reporter) End(status, message string) {
	r.events <- Event{
		Type:   EventTaskEnd,
		TS:     time.Now(),
		TaskID: r.taskID,
		Data:   map[string]any{"status": status, "message": message},
	}
}

// Pause sends an EventPause event.
func (r *Reporter) Pause() {
	r.events <- Event{
		Type:   EventPause,
		TS:     time.Now(),
		TaskID: r.taskID,
	}
}

// Resume sends an EventResume event.
func (r *Reporter) Resume() {
	r.events <- Event{
		Type:   EventResume,
		TS:     time.Now(),
		TaskID: r.taskID,
	}
}
