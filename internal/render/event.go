package render

import "time"

// Event type constants.
const (
	EventTaskStart = "task.start"
	EventTaskLog   = "task.log"
	EventTaskEnd   = "task.end"
	EventPause     = "renderer.pause"
	EventResume    = "renderer.resume"
)

// Event represents a single occurrence in the render pipeline.
type Event struct {
	Type   string    `json:"type"`
	TS     time.Time `json:"ts"`
	Seq    uint64    `json:"seq"`
	TaskID string    `json:"task_id"`
	Data   any       `json:"data,omitempty"`
}
