package render

// TaskResult holds the outcome of a single task.
type TaskResult struct {
	TaskID  string
	Status  string
	Message string
}

// JSONRenderer collects task end results for structured output.
type JSONRenderer struct {
	results []TaskResult
}

// Init pre-allocates the results slice.
func (r *JSONRenderer) Init(taskCount int) {
	r.results = make([]TaskResult, 0, taskCount)
}

// HandleEvent processes an event. Only EventTaskEnd events are collected;
// all other event types are ignored.
func (r *JSONRenderer) HandleEvent(e Event) {
	if e.Type != EventTaskEnd {
		return
	}

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
}

// Flush is a no-op for JSONRenderer.
func (r *JSONRenderer) Flush() {}

// Pause is a no-op for JSONRenderer.
func (r *JSONRenderer) Pause() {}

// Resume is a no-op for JSONRenderer.
func (r *JSONRenderer) Resume() {}

// Finish is a no-op for JSONRenderer.
func (r *JSONRenderer) Finish() {}

// Results returns the collected task results.
func (r *JSONRenderer) Results() []TaskResult {
	return r.results
}
