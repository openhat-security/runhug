package cli

import (
	"bufio"
	"strconv"
	"strings"
	"unicode/utf8"
)

type keyKind int

const (
	keyRune keyKind = iota
	keyUp
	keyDown
	keyLeft
	keyRight
	keyPasteStart
	keyPasteEnd
	keyMouse
	keyCPR
	keyPageUp
	keyPageDown
	keyIgnore
)

type mouseEv struct {
	btn     int
	x, y    int
	motion  bool
	release bool
	wheel   int // -1 up, +1 down, 0 none
}

type keyEvent struct {
	kind  keyKind
	r     rune
	mouse mouseEv
	cprX  int
	cprY  int
}

func readKeyEvent(r *bufio.Reader) (keyEvent, error) {
	b, err := r.ReadByte()
	if err != nil {
		return keyEvent{}, err
	}
	if b == 0x1b {
		return readEscEvent(r)
	}
	if b < utf8.RuneSelf {
		return keyEvent{kind: keyRune, r: rune(b)}, nil
	}
	var buf [utf8.UTFMax]byte
	buf[0] = b
	n := 1
	for !utf8.FullRune(buf[:n]) && n < utf8.UTFMax {
		nb, err := r.ReadByte()
		if err != nil {
			return keyEvent{kind: keyRune, r: utf8.RuneError}, err
		}
		buf[n] = nb
		n++
	}
	rn, _ := utf8.DecodeRune(buf[:n])
	return keyEvent{kind: keyRune, r: rn}, nil
}

func readEscEvent(r *bufio.Reader) (keyEvent, error) {
	b, err := r.ReadByte()
	if err != nil {
		return keyEvent{kind: keyIgnore}, err
	}
	if b != '[' {
		return keyEvent{kind: keyIgnore}, nil
	}
	var params []byte
	for {
		c, err := r.ReadByte()
		if err != nil {
			return keyEvent{}, err
		}
		if (c >= '0' && c <= '9') || c == ';' || c == '<' || c == '?' {
			params = append(params, c)
			continue
		}
		return parseCSI(string(params), c), nil
	}
}

func parseCSI(params string, final byte) keyEvent {
	switch final {
	case 'A':
		return keyEvent{kind: keyUp}
	case 'B':
		return keyEvent{kind: keyDown}
	case 'C':
		return keyEvent{kind: keyRight}
	case 'D':
		return keyEvent{kind: keyLeft}
	case 'R':
		parts := strings.Split(params, ";")
		if len(parts) != 2 {
			return keyEvent{kind: keyIgnore}
		}
		y, err1 := strconv.Atoi(parts[0])
		x, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return keyEvent{kind: keyIgnore}
		}
		return keyEvent{kind: keyCPR, cprX: x, cprY: y}
	case 'M', 'm':
		if ev, ok := parseSGRMouse(params, final); ok {
			return keyEvent{kind: keyMouse, mouse: ev}
		}
		return keyEvent{kind: keyIgnore}
	case '~':
		switch params {
		case "5":
			return keyEvent{kind: keyPageUp}
		case "6":
			return keyEvent{kind: keyPageDown}
		case "200":
			return keyEvent{kind: keyPasteStart}
		case "201":
			return keyEvent{kind: keyPasteEnd}
		default:
			return keyEvent{kind: keyIgnore}
		}
	default:
		return keyEvent{kind: keyIgnore}
	}
}

func parseSGRMouse(params string, final byte) (mouseEv, bool) {
	if !strings.HasPrefix(params, "<") {
		return mouseEv{}, false
	}
	parts := strings.Split(strings.TrimPrefix(params, "<"), ";")
	if len(parts) != 3 {
		return mouseEv{}, false
	}
	btn, err1 := strconv.Atoi(parts[0])
	x, err2 := strconv.Atoi(parts[1])
	y, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return mouseEv{}, false
	}
	code := btn
	if code >= 64 {
		low := code & 3
		if low <= 1 {
			wheel := -1
			if low == 1 {
				wheel = 1
			}
			return mouseEv{x: x, y: y, wheel: wheel}, true
		}
		return mouseEv{}, false
	}
	ev := mouseEv{x: x, y: y, release: final == 'm'}
	if btn&32 != 0 {
		ev.motion = true
		ev.btn = (btn - 32) & 3
	} else {
		ev.btn = btn & 3
	}
	return ev, true
}

func readBracketedPaste(r *bufio.Reader) (string, error) {
	var b strings.Builder
	for {
		ev, err := readKeyEvent(r)
		if err != nil {
			return b.String(), err
		}
		switch ev.kind {
		case keyPasteEnd:
			return b.String(), nil
		case keyRune:
			b.WriteRune(ev.r)
		}
	}
}

func pasteRunes(s string) []rune {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			out = append(out, ' ')
		case r < 32:
			continue
		default:
			out = append(out, r)
		}
	}
	return out
}

func insertRunes(buf []rune, cursor int, add []rune) ([]rune, int) {
	if len(add) == 0 {
		return buf, cursor
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(buf) {
		cursor = len(buf)
	}
	out := make([]rune, 0, len(buf)+len(add))
	out = append(out, buf[:cursor]...)
	out = append(out, add...)
	out = append(out, buf[cursor:]...)
	return out, cursor + len(add)
}
