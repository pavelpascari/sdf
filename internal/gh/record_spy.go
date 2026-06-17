//go:build spyrecord

package gh

import (
	"os"
	"time"

	"github.com/pavelpascari/sdf/internal/spy"
)

var spyRec *spy.Recorder
var fullSpy *spy.Recorder

func init() {
	if dir := os.Getenv("SDF_SPY_DIR"); dir != "" {
		spyRec = spy.NewRecorderFor(dir, "sdf", "gh")
		fullSpy = spy.NewRecorder(dir, "full")
	}
}

func recordRun(args []string, output string, exitCode int, elapsed time.Duration) {
	if spyRec != nil {
		spyRec.Record(args, output, exitCode, elapsed)
		fullSpy.RecordAs("sdf", "gh", args, output, exitCode, elapsed)
	}
}
