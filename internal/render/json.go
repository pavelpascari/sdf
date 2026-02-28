package render

// TaskResult holds the outcome of a single task.
type TaskResult struct {
	TaskID  string
	Status  string
	Message string
}

// JSONRenderer collects task end results for structured output.
type JSONRenderer struct {
	results  []TaskResult
	warnings []string
	errors   []string
}

// HandleEvent processes an event. EventTaskEnd, EventWarn, and EventErr
// events are collected; all other event types are ignored.
func (r *JSONRenderer) HandleEvent(e Event) {
	switch e.Type {
	case EventTaskEnd:
		data, ok := e.Data.(map[string]any)
		if !ok {
			return
		}
		status, _ := data["status"].(string)
		message, _ := data["message"].(string)
		r.results = append(r.results, TaskResult{
			TaskID:  e.TaskID,
			Status:  status,
			Message: message,
		})
	case EventWarn:
		data, ok := e.Data.(map[string]any)
		if !ok {
			return
		}
		text, _ := data["text"].(string)
		r.warnings = append(r.warnings, text)
	case EventErr:
		data, ok := e.Data.(map[string]any)
		if !ok {
			return
		}
		text, _ := data["text"].(string)
		r.errors = append(r.errors, text)
	}
}

// Flush is a no-op for JSONRenderer.
func (r *JSONRenderer) Flush() {}

// Finish is a no-op for JSONRenderer.
func (r *JSONRenderer) Finish() {}

// Results returns the collected task results.
func (r *JSONRenderer) Results() []TaskResult {
	return r.results
}

// Warnings returns the collected warning messages.
func (r *JSONRenderer) Warnings() []string {
	return r.warnings
}

// Errors returns the collected error messages.
func (r *JSONRenderer) Errors() []string {
	return r.errors
}
