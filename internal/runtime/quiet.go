package runtime

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// DiscardChildIO sends a long-lived child process's stdout/stderr to os.DevNull
// so server spam (GIN, llama-server INFO) never hits the user's TTY.
// Set RUNHUG_VERBOSE=1 to keep child logs on stderr.
func DiscardChildIO(cmd *exec.Cmd) (cleanup func(), err error) {
	return discardChildIO(cmd)
}

func discardChildIO(cmd *exec.Cmd) (cleanup func(), err error) {
	if Verbose() {
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return func() {}, nil
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	return func() { _ = devNull.Close() }, nil
}

// QuietWriter is os.Stderr when RUNHUG_VERBOSE is set, else io.Discard.
func QuietWriter() io.Writer {
	if Verbose() {
		return os.Stderr
	}
	return io.Discard
}
