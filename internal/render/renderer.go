package render

// Renderer defines a pluggable output backend for the event bus.
type Renderer interface {
	HandleEvent(Event)
	Flush()
	Finish()
}
