package process

import (
	"errors"
	"fmt"
	"os"
	"time"
)

const processOutputDrainTimeout = 2 * time.Second

var ErrOutputDrainTimeout = errors.New("process output drain deadline exceeded; trailing output may be incomplete")

// Wait must reap the leader concurrently with reading. Its pipe helpers close
// readers on exit and can discard buffered final output; our owned readers get
// a separate budget after leader reaping and group termination complete, so
// inherited descriptors cannot consume the group grace or stall EOF forever.
func waitForProcessOutput(completed <-chan struct{}, stdout, stderr *os.File) error {
	timer := time.NewTimer(processOutputDrainTimeout)
	defer timer.Stop()
	remaining := 2
	for remaining > 0 {
		select {
		case <-completed:
			remaining--
		case <-timer.C:
			_ = stdout.Close()
			_ = stderr.Close()
			for remaining > 0 {
				<-completed
				remaining--
			}
			return fmt.Errorf("%w after %s", ErrOutputDrainTimeout, processOutputDrainTimeout)
		}
	}
	return nil
}
