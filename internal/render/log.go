package render

import (
	"bufio"
	"encoding/json"
	"io"
)

// LogWriter writes events as newline-delimited JSON (JSONL) to an io.Writer.
// It is used by the Bus router to tee events to .sdf/logs/*.jsonl.
type LogWriter struct {
	bw  *bufio.Writer
	enc *json.Encoder
}

// NewLogWriter creates a LogWriter that buffers output to w.
func NewLogWriter(w io.Writer) *LogWriter {
	bw := bufio.NewWriterSize(w, 64*1024)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	return &LogWriter{bw: bw, enc: enc}
}

// Write encodes a single event as a JSON line.
func (l *LogWriter) Write(ev Event) error {
	return l.enc.Encode(ev)
}

// Flush flushes the buffered writer to the underlying io.Writer.
func (l *LogWriter) Flush() error {
	return l.bw.Flush()
}
