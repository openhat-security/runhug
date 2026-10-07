package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/config"
	"golang.org/x/term"
)

const runHistoryLimit = 200

// runLineEditor is a minimal raw-mode prompt: history, Ctrl-C clear, `/` hints, Tab complete.
type runLineEditor struct {
	In            *os.File
	Out           io.Writer
	Prompt        string
	History       []string
	Cwd           string
	Capture       *lineBuf
	regionTop     int
	regionBot     int
	suggestN      int // hint rows last drawn
	drawRows      int // physical rows of last redraw (input wraps + hints)
	cursorRow     int
	suppressHints bool
	curX, curY    int
	sel           *selRange
	dragging      bool
	CaptureMouse  bool
	scrollOff     int
}

func newRunLineEditor(prompt string, history []string) *runLineEditor {
	return &runLineEditor{
		In:      os.Stdin,
		Out:     os.Stdout,
		Prompt:  prompt,
		History: append([]string{}, history...),
	}
}

func loadRunInputHistory() []string {
	dir, err := config.Dir()
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, "run_history"))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	if len(out) > runHistoryLimit {
		out = out[len(out)-runHistoryLimit:]
	}
	return out
}

func saveRunInputHistory(hist []string) {
	dir, err := config.Dir()
	if err != nil {
		return
	}
	if len(hist) > runHistoryLimit {
		hist = hist[len(hist)-runHistoryLimit:]
	}
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "run_history"), []byte(strings.Join(hist, "\n")+"\n"), 0o600)
}

// ReadLine returns (line, eof, err). eof on Ctrl-D with empty buffer.
func (e *runLineEditor) ReadLine() (string, bool, error) {
	fd := int(e.In.Fd())
	if !term.IsTerminal(fd) {
		return "", true, fmt.Errorf("stdin is not a terminal")
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return "", false, err
	}
	defer func() {
		fmt.Fprint(e.Out, "\033[?2004l\033[?1000l\033[?1002l\033[?1006l\033[?1007l\033[?7h")
		_ = term.Restore(fd, old)
	}()
	fmt.Fprint(e.Out, "\033[?7l\033[?2004h\033[?1007l")
	if e.CaptureMouse {
		fmt.Fprint(e.Out, "\033[?1000h\033[?1002h\033[?1006h")
	}

	br := bufio.NewReader(e.In)
	buf := []rune{}
	cursor := 0
	histIdx := len(e.History) // one past end = drafting new line
	draft := ""
	e.redraw(buf, cursor)
	fmt.Fprint(e.Out, "\033[6n")

	for {
		ev, err := readKeyEvent(br)
		if err != nil {
			e.clearSuggestions()
			return "", false, err
		}
		switch ev.kind {
		case keyIgnore:
			continue
		case keyCPR:
			e.curX, e.curY = ev.cprX, ev.cprY
			continue
		case keyPasteStart:
			text, err := readBracketedPaste(br)
			if err != nil {
				e.clearSuggestions()
				return "", false, err
			}
			buf, cursor = insertRunes(buf, cursor, pasteRunes(text))
			histIdx = len(e.History)
			e.redraw(buf, cursor)
			fmt.Fprint(e.Out, "\033[6n")
			continue
		case keyMouse:
			if ev.mouse.wheel != 0 {
				e.scrollOff -= ev.mouse.wheel * 3
				e.paintChatWindow(buf, cursor)
				continue
			}
			var dirty bool
			buf, cursor, dirty = e.handleMouse(ev.mouse, buf, cursor)
			if dirty {
				histIdx = len(e.History)
				e.redraw(buf, cursor)
				fmt.Fprint(e.Out, "\033[6n")
			}
			continue
		case keyPageUp:
			e.scrollOff += 8
			e.paintChatWindow(buf, cursor)
			continue
		case keyPageDown:
			e.scrollOff -= 8
			e.paintChatWindow(buf, cursor)
			continue
		case keyUp:
			if len(e.History) == 0 {
				continue
			}
			if histIdx == len(e.History) {
				draft = string(buf)
			}
			if histIdx > 0 {
				histIdx--
				buf = []rune(e.History[histIdx])
				cursor = len(buf)
				e.redraw(buf, cursor)
				fmt.Fprint(e.Out, "\033[6n")
			}
			continue
		case keyDown:
			if histIdx < len(e.History) {
				histIdx++
				if histIdx == len(e.History) {
					buf = []rune(draft)
				} else {
					buf = []rune(e.History[histIdx])
				}
				cursor = len(buf)
				e.redraw(buf, cursor)
				fmt.Fprint(e.Out, "\033[6n")
			}
			continue
		case keyRight:
			if cursor < len(buf) {
				cursor++
				e.redraw(buf, cursor)
				fmt.Fprint(e.Out, "\033[6n")
			}
			continue
		case keyLeft:
			if cursor > 0 {
				cursor--
				e.redraw(buf, cursor)
				fmt.Fprint(e.Out, "\033[6n")
			}
			continue
		}

		c := ev.r
		switch c {
		case 3: // Ctrl-C — clear line, never exit
			buf = buf[:0]
			cursor = 0
			histIdx = len(e.History)
			draft = ""
			e.sel = nil
			e.redraw(buf, cursor)
			fmt.Fprint(e.Out, "\033[6n")
			continue
		case 4: // Ctrl-D
			if len(buf) == 0 {
				e.finishInputDraw()
				fmt.Fprint(e.Out, "\r\n")
				return "", true, nil
			}
			if cursor < len(buf) {
				buf = append(buf[:cursor], buf[cursor+1:]...)
				e.redraw(buf, cursor)
				fmt.Fprint(e.Out, "\033[6n")
			}
			continue
		case 22: // Ctrl-V paste
			if clip, err := pasteFromClipboard(); err == nil {
				buf, cursor = insertRunes(buf, cursor, pasteRunes(clip))
				histIdx = len(e.History)
				e.redraw(buf, cursor)
				fmt.Fprint(e.Out, "\033[6n")
			}
			continue
		case 127, 8: // backspace
			if cursor > 0 {
				buf = append(buf[:cursor-1], buf[cursor:]...)
				cursor--
				e.redraw(buf, cursor)
				fmt.Fprint(e.Out, "\033[6n")
			}
			continue
		case '\t':
			line := string(buf)
			if strings.HasPrefix(line, "/") {
				if done, ok := completeInputLine(e.Cwd, line); ok {
					buf = []rune(done)
					cursor = len(buf)
				}
				e.redraw(buf, cursor)
				fmt.Fprint(e.Out, "\033[6n")
			}
			continue
		case '\r', '\n':
			e.sel = nil
			e.suppressHints = true
			e.redraw(buf, len(buf))
			e.suppressHints = false
			e.finishInputDraw()
			fmt.Fprint(e.Out, "\r\n")
			line := strings.TrimSpace(string(buf))
			return line, false, nil
		default:
			if c < 32 {
				continue
			}
			e.scrollOff = 0
			buf, cursor = insertRunes(buf, cursor, []rune{c})
			histIdx = len(e.History)
			e.redraw(buf, cursor)
			fmt.Fprint(e.Out, "\033[6n")
		}
	}
}

func (e *runLineEditor) PushHistory(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if n := len(e.History); n > 0 && e.History[n-1] == line {
		return
	}
	e.History = append(e.History, line)
	if len(e.History) > runHistoryLimit {
		e.History = e.History[len(e.History)-runHistoryLimit:]
	}
	saveRunInputHistory(e.History)
}

func (e *runLineEditor) redraw(buf []rune, cursor int) {
	width := e.termWidth()
	promptW := visibleLen(e.Prompt)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(buf) {
		cursor = len(buf)
	}

	e.moveToDrawStart()
	nClear := e.drawRows
	if nClear < 1 {
		nClear = 1
	}
	for i := 0; i < nClear; i++ {
		fmt.Fprint(e.Out, "\r\033[2K")
		if i < nClear-1 {
			fmt.Fprint(e.Out, "\r\n")
		}
	}
	if nClear > 1 {
		fmt.Fprintf(e.Out, "\033[%dA\r", nClear-1)
	} else {
		fmt.Fprint(e.Out, "\r")
	}

	display := e.Prompt + string(buf)
	rows := wrapANSI(display, width)
	abs := promptW + cursor
	if abs > 0 && abs%width == 0 && cursor == len(buf) {
		rows = append(rows, "")
	}
	line := string(buf)
	hints := []string(nil)
	if !e.suppressHints {
		hints = suggestionLines(e.Cwd, line, width)
	}
	for i, row := range rows {
		if i > 0 {
			fmt.Fprint(e.Out, "\r\n")
		}
		fmt.Fprint(e.Out, row)
	}
	for _, h := range hints {
		fmt.Fprintf(e.Out, "\r\n\033[2K%s", dim(fitVisible(h, width)))
	}
	e.suggestN = len(hints)
	e.drawRows = len(rows) + len(hints)
	if e.drawRows < 1 {
		e.drawRows = 1
	}
	e.cursorRow = 0
	if width > 0 {
		e.cursorRow = abs / width
	}
	ccol := 0
	if width > 0 {
		ccol = abs % width
	}

	up := e.drawRows - 1 - e.cursorRow
	if up > 0 {
		fmt.Fprintf(e.Out, "\033[%dA", up)
	}
	fmt.Fprint(e.Out, "\r")
	if ccol > 0 {
		fmt.Fprintf(e.Out, "\033[%dC", ccol)
	}
	if e.sel != nil {
		e.paintSelection(buf)
		e.restoreCursor(buf, cursor)
	}
}

func (e *runLineEditor) moveToDrawStart() {
	if e.cursorRow > 0 {
		fmt.Fprintf(e.Out, "\033[%dA", e.cursorRow)
	}
	fmt.Fprint(e.Out, "\r")
}

func (e *runLineEditor) finishInputDraw() {
	down := e.drawRows - 1 - e.cursorRow
	if e.drawRows > 1 && down > 0 {
		fmt.Fprintf(e.Out, "\033[%dB", down)
	}
	e.suggestN = 0
	e.drawRows = 0
	e.cursorRow = 0
}

func wrapANSI(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var rows []string
	var b strings.Builder
	vis := 0
	inEsc := false
	for _, r := range s {
		if inEsc {
			b.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		if vis >= width {
			rows = append(rows, b.String())
			b.Reset()
			vis = 0
		}
		b.WriteRune(r)
		vis++
	}
	rows = append(rows, b.String())
	if len(rows) == 0 {
		return []string{""}
	}
	return rows
}

func (e *runLineEditor) termWidth() int {
	if e.In != nil {
		if w, _, err := term.GetSize(int(e.In.Fd())); err == nil && w >= 20 {
			return w
		}
	}
	return 80
}

func suggestionLines(cwd, line string, width int) []string {
	if arg, ok := cwdArg(line); ok {
		_, names := cwdDirMatches(cwd, arg)
		if len(names) == 0 {
			return nil
		}
		return formatLSColumns(names, width)
	}
	if strings.HasPrefix(line, "/") && !strings.Contains(line, " ") {
		matches := filterSlashCatalog(line)
		if len(matches) == 0 {
			return nil
		}
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, m.Cmd)
		}
		return formatLSColumns(names, width)
	}
	return nil
}

// formatLSColumns lays names out down-then-across, like coreutils ls.
func formatLSColumns(names []string, width int) []string {
	if len(names) == 0 {
		return nil
	}
	if width < 8 {
		width = 8
	}
	indent := 2
	max := 0
	for _, n := range names {
		if l := displayWidth(n); l > max {
			max = l
		}
	}
	gap := 2
	colW := max + gap
	avail := width - indent
	if avail < 1 {
		avail = 1
	}
	cols := avail / colW
	if cols < 1 {
		cols = 1
		colW = avail
	}
	for cols > 1 && indent+(cols-1)*colW+max > width {
		cols--
	}
	if cols > len(names) {
		cols = len(names)
	}
	rows := (len(names) + cols - 1) / cols
	out := make([]string, rows)
	pad := strings.Repeat(" ", indent)
	for r := 0; r < rows; r++ {
		var b strings.Builder
		b.WriteString(pad)
		for c := 0; c < cols; c++ {
			i := r + c*rows
			if i >= len(names) {
				continue
			}
			name := names[i]
			last := c == cols-1 || r+(c+1)*rows >= len(names)
			if last {
				b.WriteString(name)
			} else {
				b.WriteString(padRight(name, colW))
			}
		}
		line := b.String()
		if displayWidth(line) > width {
			line = truncateRunes(line, width)
		}
		out[r] = line
	}
	return out
}

func displayWidth(s string) int {
	return visibleLen(s)
}

func suggestionLine(cwd, line string) string {
	lines := suggestionLines(cwd, line, 80)
	return strings.TrimSpace(strings.Join(lines, " "))
}

func (e *runLineEditor) clearSuggestions() {
	e.finishInputDraw()
}

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func visibleLen(s string) int {
	return len([]rune(stripANSI(s)))
}
