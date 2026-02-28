package render

// Renderer defines a pluggable output backend for the event bus.
type Renderer interface {
	Init(taskCount int)
	HandleEvent(Event)
	Flush()
	Pause()
	Resume()
	Finish()
}
