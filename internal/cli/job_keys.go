package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

type quietLog struct {
	mu   sync.Mutex
	file *os.File
	live bool
}

func (q *quietLog) Write(p []byte) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	n, err := q.file.Write(p)
	if q.live {
		_, _ = os.Stderr.Write(p)
	}
	return n, err
}

func (q *quietLog) showVerbose() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.live {
		return
	}
	q.live = true
	_, _ = q.file.Seek(0, 0)
	_, _ = io.Copy(os.Stderr, q.file)
}

func (q *quietLog) setLive() {
	q.mu.Lock()
	q.live = true
	q.mu.Unlock()
}

func classifyJobKey(b byte) string {
	switch b {
	case 'v', 'V':
		return "verbose"
	case 'h', 'H':
		return "hurry"
	case 'c', 'C', 3:
		return "cancel"
	case '\r', '\n':
		return ""
	default:
		return ""
	}
}

func jobKeysHint() string {
	return dim("v verbose · h hurry · c cancel")
}

func printJobElapsed(started time.Time, n, total int, label, detail string) {
	printEnsureStep(os.Stderr, n, total, label, fmt.Sprintf("%s  %s  %s",
		detail, dim(time.Since(started).Truncate(time.Second).String()), jobKeysHint()))
}

func startJobKeys(ctx context.Context, cancel context.CancelFunc, log *quietLog) func() {
	if !stdinIsTTY() || !term.IsTerminal(int(os.Stdin.Fd())) {
		return func() {}
	}
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return func() {}
	}
	_ = setStdinNonblock(true)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 8)
		for ctx.Err() == nil {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				for _, b := range buf[:n] {
					switch classifyJobKey(b) {
					case "verbose":
						fmt.Fprint(os.Stderr, "\r\n")
						fmt.Fprintln(os.Stderr, dim("verbose"))
						log.showVerbose()
					case "hurry":
						fmt.Fprint(os.Stderr, "\r\n")
						fmt.Fprintln(os.Stderr, "We're trying..")
					case "cancel":
						cancel()
						return
					}
				}
				continue
			}
			if err != nil && ctx.Err() == nil {
				time.Sleep(120 * time.Millisecond)
			}
		}
	}()
	return func() {
		_ = setStdinNonblock(false)
		_ = term.Restore(fd, old)
		// goroutine exits on ctx cancel from caller
	}
}
