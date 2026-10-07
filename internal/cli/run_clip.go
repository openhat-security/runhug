package cli

import (
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"
)

const lineBufMax = 4000

type lineBuf struct {
	mu    sync.Mutex
	lines []string
	cur   strings.Builder
}

func (b *lineBuf) Write(p []byte) (int, error) {
	n := len(p)
	if b == nil {
		return n, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for len(p) > 0 {
		r, n := utf8.DecodeRune(p)
		p = p[n:]
		switch r {
		case '\n':
			b.lines = append(b.lines, b.cur.String())
			b.cur.Reset()
			if len(b.lines) > lineBufMax {
				b.lines = b.lines[len(b.lines)-lineBufMax:]
			}
		case '\r':
			b.cur.Reset()
		case 0x1b:
			// drop CSI / OSC
			for len(p) > 0 {
				r2, n2 := utf8.DecodeRune(p)
				p = p[n2:]
				if r2 == 0x1b {
					p = append([]byte(string(r2)), p...)
					break
				}
				if (r2 >= 'a' && r2 <= 'z') || (r2 >= 'A' && r2 <= 'Z') || r2 == '\a' {
					break
				}
			}
		default:
			if r >= 32 {
				b.cur.WriteRune(r)
			}
		}
	}
	return n, nil
}

func (b *lineBuf) Snapshot() []string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := append([]string{}, b.lines...)
	if b.cur.Len() > 0 {
		out = append(out, b.cur.String())
	}
	return out
}

type captureWriter struct {
	w   io.Writer
	buf *lineBuf
}

func (c *captureWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if c.buf != nil {
		_, _ = c.buf.Write(p[:n])
	}
	return n, err
}

func (c *captureWriter) Sync() error {
	if s, ok := c.w.(interface{ Sync() error }); ok {
		return s.Sync()
	}
	return nil
}

func (e *runLineEditor) copyText(text string) {
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return
	}
	_ = copyToClipboard(text)
	if len(text) > 64*1024 {
		text = text[:64*1024]
	}
	fmt.Fprintf(e.Out, "\033]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(text)))
}

func (e *runLineEditor) handleMouse(ev mouseEv, buf []rune, cursor int) ([]rune, int, bool) {
	if ev.wheel != 0 {
		return buf, cursor, false
	}
	if ev.btn == 1 && ev.release && !ev.motion {
		if clip, err := pasteFromClipboard(); err == nil {
			buf, cursor = insertRunes(buf, cursor, pasteRunes(clip))
			e.sel = nil
			e.dragging = false
			return buf, cursor, true
		}
		return buf, cursor, false
	}
	if ev.btn != 0 {
		return buf, cursor, false
	}
	if !ev.motion && !ev.release {
		e.sel = &selRange{x1: ev.x, y1: ev.y, x2: ev.x, y2: ev.y}
		e.dragging = true
		return buf, cursor, true
	}
	if e.sel == nil {
		return buf, cursor, false
	}
	e.sel.x2, e.sel.y2 = ev.x, ev.y
	if ev.release {
		e.dragging = false
		if text := e.selectedText(buf); text != "" {
			e.copyText(text)
		}
		if e.sel.x1 == e.sel.x2 && e.sel.y1 == e.sel.y2 {
			e.sel = nil
		}
	}
	return buf, cursor, true
}

type selRange struct {
	x1, y1, x2, y2 int
}

func (e *runLineEditor) selectedText(buf []rune) string {
	if e.sel == nil {
		return ""
	}
	rows, originY := e.visForMouse(buf)
	return extractScreenRect(rows, originY, e.sel.x1, e.sel.y1, e.sel.x2, e.sel.y2)
}

func extractScreenRect(rows []string, originY, x1, y1, x2, y2 int) string {
	if y1 > y2 || (y1 == y2 && x1 > x2) {
		x1, y1, x2, y2 = x2, y2, x1, y1
	}
	var b strings.Builder
	for y := y1; y <= y2; y++ {
		idx := y - originY
		if idx < 0 || idx >= len(rows) {
			if y < y2 {
				b.WriteByte('\n')
			}
			continue
		}
		rs := []rune(rows[idx])
		start, end := 0, len(rs)
		if y == y1 {
			start = x1 - 1
			if start < 0 {
				start = 0
			}
			if start > len(rs) {
				start = len(rs)
			}
		}
		if y == y2 {
			end = x2
			if end < start {
				end = start
			}
			if end > len(rs) {
				end = len(rs)
			}
		}
		b.WriteString(string(rs[start:end]))
		if y < y2 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (e *runLineEditor) visForMouse(buf []rune) ([]string, int) {
	chat, input, _ := e.visibleChatAndInput(buf)
	top, _ := e.region()
	return append(append([]string{}, chat...), input...), top
}

func (e *runLineEditor) visibleChatAndInput(buf []rune) (chat, input []string, chatH int) {
	width := e.termWidth()
	top, bot := e.region()
	input = wrapANSI(stripANSI(e.Prompt)+string(buf), width)
	abs := visibleLen(e.Prompt) + len(buf)
	if abs > 0 && width > 0 && abs%width == 0 {
		input = append(input, "")
	}
	inRows := len(input)
	if inRows < 1 {
		inRows = 1
	}
	h := bot - top + 1
	chatH = h - inRows
	if chatH < 1 {
		chatH = 1
	}
	var vis []string
	if e.Capture != nil {
		for _, ln := range e.Capture.Snapshot() {
			vis = append(vis, wrapANSI(stripANSI(ln), width)...)
		}
	}
	maxOff := len(vis) - chatH
	if maxOff < 0 {
		maxOff = 0
	}
	if e.scrollOff > maxOff {
		e.scrollOff = maxOff
	}
	if e.scrollOff < 0 {
		e.scrollOff = 0
	}
	end := len(vis) - e.scrollOff
	if end < 0 {
		end = 0
	}
	if end > len(vis) {
		end = len(vis)
	}
	start := end - chatH
	if start < 0 {
		start = 0
	}
	chat = make([]string, chatH)
	for i := 0; i < chatH; i++ {
		idx := start + i
		if idx >= start && idx < end && idx < len(vis) {
			chat[i] = vis[idx]
		}
	}
	return chat, input, chatH
}

func (e *runLineEditor) region() (top, bot int) {
	_, h := termWH()
	top, bot = 1, h
	if e.regionTop > 0 {
		top = e.regionTop
	}
	if e.regionBot > 0 {
		bot = e.regionBot
	}
	if bot < top {
		bot = top
	}
	return top, bot
}

func (e *runLineEditor) paintSelection(buf []rune) {
	if e.sel == nil {
		return
	}
	rows, originY := e.visForMouse(buf)
	x1, y1, x2, y2 := e.sel.x1, e.sel.y1, e.sel.x2, e.sel.y2
	if y1 > y2 || (y1 == y2 && x1 > x2) {
		x1, y1, x2, y2 = x2, y2, x1, y1
	}
	for y := y1; y <= y2; y++ {
		idx := y - originY
		if idx < 0 || idx >= len(rows) {
			continue
		}
		rs := []rune(rows[idx])
		start, end := 0, len(rs)
		if y == y1 {
			start = x1 - 1
			if start < 0 {
				start = 0
			}
		}
		if y == y2 {
			end = x2
			if end < start {
				end = start
			}
		}
		if start > len(rs) {
			start = len(rs)
		}
		if end > len(rs) {
			end = len(rs)
		}
		fmt.Fprintf(e.Out, "\033[%d;1H\033[2K", y)
		fmt.Fprint(e.Out, string(rs[:start]))
		if end > start {
			fmt.Fprint(e.Out, "\033[7m"+string(rs[start:end])+"\033[0m")
		}
		fmt.Fprint(e.Out, string(rs[end:]))
	}
}

func (e *runLineEditor) restoreCursor(buf []rune, cursor int) {
	width := e.termWidth()
	promptW := visibleLen(e.Prompt)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(buf) {
		cursor = len(buf)
	}
	abs := promptW + cursor
	ccol := 0
	crow := 0
	if width > 0 {
		crow = abs / width
		ccol = abs % width
	}
	if e.curY > 0 {
		y := e.curY - e.cursorRow + crow
		x := ccol + 1
		if x < 1 {
			x = 1
		}
		fmt.Fprintf(e.Out, "\033[%d;%dH", y, x)
		return
	}
	fmt.Fprint(e.Out, "\r")
	if ccol > 0 {
		fmt.Fprintf(e.Out, "\033[%dC", ccol)
	}
}

func (e *runLineEditor) paintChatWindow(buf []rune, cursor int) {
	if e == nil || !e.CaptureMouse {
		return
	}
	top, _ := e.region()
	chat, _, chatH := e.visibleChatAndInput(buf)
	fmt.Fprintf(e.Out, "\033[%d;1H", top)
	for i := 0; i < chatH; i++ {
		fmt.Fprint(e.Out, "\r\033[2K")
		if i < len(chat) && chat[i] != "" {
			fmt.Fprint(e.Out, chat[i])
		}
		fmt.Fprint(e.Out, "\r\n")
	}
	e.drawRows = 0
	e.cursorRow = 0
	fmt.Fprintf(e.Out, "\033[%d;1H", top+chatH)
	e.redraw(buf, cursor)
}
