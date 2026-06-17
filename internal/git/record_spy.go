//go:build spyrecord

package git

import (
	"os"
	"time"

	"github.com/pavelpascari/sdf/internal/spy"
)

var spyRec *spy.Recorder
var fullSpy *spy.Recorder

func init() {
	if dir := os.Getenv("SDF_SPY_DIR"); dir != "" {
		spyRec = spy.NewRecorderFor(dir, "sdf", "git")
		fullSpy = spy.NewRecorder(dir, "full")
	}
}

func recordRun(args []string, output string, exitCode int, elapsed time.Duration) {
	if spyRec != nil {
		spyRec.Record(args, output, exitCode, elapsed)
		fullSpy.RecordAs("sdf", "git", args, output, exitCode, elapsed)
	}
}
