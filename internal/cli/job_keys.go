package cli

import (
	"bytes"
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
	// raw is set when stdin is in term.MakeRaw. Output then needs CRLF;
	// a bare LF leaves the cursor in the same column and staircases.
	raw bool
}

func (q *quietLog) Write(p []byte) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	n, err := q.file.Write(p)
	if q.live {
		q.writeErr(p)
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
	buf, err := io.ReadAll(q.file)
	if err != nil {
		return
	}
	q.writeErr(buf)
}

// writeErr prints docker output. Caller holds q.mu.
func (q *quietLog) writeErr(p []byte) {
	if q.raw {
		p = normalizeCRLF(p)
	}
	_, _ = os.Stderr.Write(p)
}

// normalizeCRLF turns bare LF into CRLF without doubling an existing CRLF.
func normalizeCRLF(p []byte) []byte {
	if !bytes.Contains(p, []byte{'\n'}) {
		return p
	}
	var b bytes.Buffer
	b.Grow(len(p) + bytes.Count(p, []byte{'\n'}))
	for i := 0; i < len(p); i++ {
		if p[i] == '\n' && (i == 0 || p[i-1] != '\r') {
			b.WriteByte('\r')
		}
		b.WriteByte(p[i])
	}
	return b.Bytes()
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

func formatJobStatus(started time.Time, n, total int, label, detail string) string {
	elapsed := time.Since(started)
	if elapsed < 0 {
		elapsed = 0
	}
	clock := elapsed.Truncate(time.Second).String()
	hint := "v verbose · h hurry · c cancel"
	plain := fmt.Sprintf("  %d/%d  %s  %s  %s  %s", n, total, padRight(label, 10), detail, clock, hint)
	value := detail + "  " + dim(clock)
	if len(plain) < jobTermWidth() {
		value += "  " + jobKeysHint()
	}
	return formatEnsureStep(n, total, label, value)
}

func jobTermWidth() int {
	if w, _, err := term.GetSize(int(os.Stderr.Fd())); err == nil && w > 0 {
		return w
	}
	return 100
}

// jobLine is the single deploy-progress row. term.MakeRaw disables ONLCR, so a
// fresh println for each tick staircases across the screen. Redraw with CR + EL
// instead, and finish with CRLF before the terminal is restored.
type jobLine struct {
	mu     sync.Mutex
	w      io.Writer
	last   string
	open   bool
	paused bool
	done   bool
}

func newJobLine(w io.Writer) *jobLine {
	return &jobLine{w: w}
}

func (l *jobLine) update(text string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.done || l.paused {
		return
	}
	if l.open {
		fmt.Fprint(l.w, "\r\033[K")
	}
	fmt.Fprint(l.w, text)
	l.last = text
	l.open = true
}

// pause ends the live row so later output (docker log, hurry note) is not erased.
func (l *jobLine) pause() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.breakOpen()
	l.paused = true
}

// finish commits the live row and ignores later ticks.
func (l *jobLine) finish() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.breakOpen()
	l.done = true
}

func (l *jobLine) breakOpen() {
	if l.open {
		fmt.Fprint(l.w, "\r\n")
		l.open = false
	}
}

func startJobKeys(ctx context.Context, cancel context.CancelFunc, log *quietLog, line *jobLine) (restore func(), raw bool) {
	noop := func() {}
	if !stdinIsTTY() || !term.IsTerminal(int(os.Stdin.Fd())) {
		return noop, false
	}
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return noop, false
	}
	log.mu.Lock()
	log.raw = true
	log.mu.Unlock()
	_ = setStdinNonblock(true)
	go func() {
		buf := make([]byte, 8)
		for ctx.Err() == nil {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				for _, b := range buf[:n] {
					switch classifyJobKey(b) {
					case "verbose":
						line.pause()
						writeRawCRLF(os.Stderr, dim("verbose")+"\n")
						log.showVerbose()
					case "hurry":
						line.pause()
						writeRawCRLF(os.Stderr, "We're trying..\n")
						line.resume()
					case "cancel":
						line.finish()
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
	}, true
}

// resume allows ticks again after a one-shot note. No-op once finish has run.
func (l *jobLine) resume() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.done {
		return
	}
	l.paused = false
	if l.last != "" {
		fmt.Fprint(l.w, l.last)
		l.open = true
	}
}
