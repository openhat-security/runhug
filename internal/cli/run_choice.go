package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

type choiceOpt struct {
	ID       string
	Label    string
	Shortcut rune // 0 = none
}

type choiceHit struct {
	Index  int
	X0, X1 int // 1-based inclusive columns (SGR mouse)
}

func layoutChoiceBar(opts []choiceOpt, selected int) (line string, hits []choiceHit) {
	if selected < 0 {
		selected = 0
	}
	if len(opts) == 0 {
		return "", nil
	}
	if selected >= len(opts) {
		selected = len(opts) - 1
	}
	var b strings.Builder
	col := 1
	for i, o := range opts {
		if i > 0 {
			b.WriteString("  ")
			col += 2
		}
		label := o.Label
		if i == selected {
			label = "[ " + o.Label + " ]"
		} else {
			label = "  " + o.Label + "  "
		}
		w := utf8.RuneCountInString(label)
		hits = append(hits, choiceHit{Index: i, X0: col, X1: col + w - 1})
		b.WriteString(label)
		col += w
	}
	return b.String(), hits
}

func hitChoice(hits []choiceHit, x int) int {
	for _, h := range hits {
		if x >= h.X0 && x <= h.X1 {
			return h.Index
		}
	}
	return -1
}

func shortcutChoice(opts []choiceOpt, r rune) int {
	if r >= 'A' && r <= 'Z' {
		r += 'a' - 'A'
	}
	for i, o := range opts {
		s := o.Shortcut
		if s >= 'A' && s <= 'Z' {
			s += 'a' - 'A'
		}
		if s != 0 && s == r {
			return i
		}
	}
	return -1
}

func wrapChoiceIndex(i, n int) int {
	if n <= 0 {
		return 0
	}
	i %= n
	if i < 0 {
		i += n
	}
	return i
}

// applyChoiceEvent updates selection. done+id when the user commits.
func applyChoiceEvent(ev keyEvent, selected int, opts []choiceOpt, hits []choiceHit) (sel int, id string, done bool) {
	n := len(opts)
	sel = selected
	if n == 0 {
		return 0, "", true
	}
	switch ev.kind {
	case keyLeft:
		return wrapChoiceIndex(sel-1, n), "", false
	case keyRight:
		return wrapChoiceIndex(sel+1, n), "", false
	case keyUp:
		return wrapChoiceIndex(sel-1, n), "", false
	case keyDown:
		return wrapChoiceIndex(sel+1, n), "", false
	case keyMouse:
		if ev.mouse.release || ev.mouse.motion || ev.mouse.wheel != 0 {
			return sel, "", false
		}
		if ev.mouse.btn != 0 {
			return sel, "", false
		}
		if i := hitChoice(hits, ev.mouse.x); i >= 0 {
			return i, opts[i].ID, true
		}
		return sel, "", false
	case keyRune:
		r := ev.r
		switch r {
		case '\r', '\n':
			return sel, opts[sel].ID, true
		case 9: // Tab
			return wrapChoiceIndex(sel+1, n), "", false
		case 27: // Esc as rune (rare; CSI handles most)
			return sel, escapeChoiceID(opts), true
		case 3: // Ctrl-C
			return sel, escapeChoiceID(opts), true
		}
		if i := shortcutChoice(opts, r); i >= 0 {
			return i, opts[i].ID, true
		}
	}
	return sel, "", false
}

func escapeChoiceID(opts []choiceOpt) string {
	for _, o := range opts {
		switch o.ID {
		case "deny", "cancel", "no":
			return o.ID
		}
	}
	if len(opts) > 0 {
		return opts[len(opts)-1].ID
	}
	return "deny"
}

func permChoiceOpts() []choiceOpt {
	return []choiceOpt{
		{ID: "allow", Label: "Allow", Shortcut: 'y'},
		{ID: "deny", Label: "Deny", Shortcut: 'n'},
		{ID: "always", Label: "Always", Shortcut: 'a'},
		{ID: "never", Label: "Never", Shortcut: 'e'},
	}
}

func resizeChoiceOpts() []choiceOpt {
	return []choiceOpt{
		{ID: "resize", Label: "Resize", Shortcut: 'y'},
		{ID: "cancel", Label: "Cancel", Shortcut: 'n'},
	}
}

// promptChoice draws OpenCode-style chips. Returns option ID.
func promptChoice(w io.Writer, in *os.File, title string, opts []choiceOpt, selected int) (string, error) {
	if w == nil {
		w = os.Stdout
	}
	if in == nil {
		in = os.Stdin
	}
	if len(opts) == 0 {
		return "deny", nil
	}
	if selected < 0 || selected >= len(opts) {
		selected = 0
	}
	if !stdinIsTTY() || !term.IsTerminal(int(in.Fd())) {
		return opts[selected].ID, nil
	}
	fd := int(in.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return opts[selected].ID, nil
	}
	defer func() {
		fmt.Fprint(w, "\033[?25h\033[?1000l\033[?1006l")
		_ = term.Restore(fd, old)
	}()
	fmt.Fprint(w, "\033[?25l\033[?1000h\033[?1006h")
	if title != "" {
		fmt.Fprintf(w, "\r\n%s\r\n", title)
	}
	br := bufio.NewReader(in)
	var hits []choiceHit
	draw := func() {
		line, h := layoutChoiceBar(opts, selected)
		hits = h
		fmt.Fprintf(w, "\r\033[K%s", line)
	}
	draw()
	for {
		ev, err := readKeyEvent(br)
		if err != nil {
			fmt.Fprint(w, "\r\n")
			return escapeChoiceID(opts), err
		}
		if ev.kind == keyIgnore || ev.kind == keyCPR {
			continue
		}
		sel, id, done := applyChoiceEvent(ev, selected, opts, hits)
		selected = sel
		if done {
			fmt.Fprint(w, "\r\n")
			return id, nil
		}
		draw()
	}
}
