package cli

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/adamsiwiec1/runhug/internal/config"
)

const (
	ansiReset      = "\033[0m"
	ansiBold       = "\033[1m"
	ansiDim        = "\033[2m"
	ansiRed        = "\033[31m"
	ansiGreen      = "\033[32m"
	ansiYellow     = "\033[33m"
	ansiBlue       = "\033[34m"
	ansiMagenta    = "\033[35m"
	ansiCyan       = "\033[36m"
	ansiBoldYellow = "\033[1;33m"
	ansiBoldCyan   = "\033[1;36m"
)

func useColor() bool {
	if config.ColorDisabled() {
		return false
	}
	return stdoutIsTTY()
}

func paint(code, s string) string {
	if s == "" || !useColor() {
		return s
	}
	return code + s + ansiReset
}

func bold(s string) string       { return paint(ansiBold, s) }
func dim(s string) string        { return paint(ansiDim, s) }
func red(s string) string        { return paint(ansiRed, s) }
func green(s string) string      { return paint(ansiGreen, s) }
func yellow(s string) string     { return paint(ansiYellow, s) }
func blue(s string) string       { return paint(ansiBlue, s) }
func magenta(s string) string    { return paint(ansiMagenta, s) }
func cyan(s string) string       { return paint(ansiCyan, s) }
func boldYellow(s string) string { return paint(ansiBoldYellow, s) }
func boldCyan(s string) string   { return paint(ansiBoldCyan, s) }

// FormatError colors an error for stderr. Never used for secrets.
func FormatError(msg string) string {
	return red(msg)
}

func heading(w io.Writer, title string) {
	fmt.Fprintln(w, boldCyan(title))
	fmt.Fprintln(w)
}

// planHeading is a yellow section title for Plan / Cost / Heretic blocks.
func planHeading(w io.Writer, title string) {
	fmt.Fprintln(w, boldYellow(title))
	fmt.Fprintln(w)
}

// helpSection prints a colored group header for root help.
func helpSection(w io.Writer, title string) {
	fmt.Fprintln(w, boldYellow(title))
}

// helpCmd prints one help row: cyan command + dim description (aligned).
func helpCmd(w io.Writer, cmd, desc string) {
	const cmdWidth = 26
	col := padRight(cmd, cmdWidth)
	if utf8.RuneCountInString(cmd) >= cmdWidth {
		col = cmd + "  "
	}
	fmt.Fprintf(w, "  %s%s\n", cyan(col), dim(desc))
}

// helpFlag prints a flag row (same alignment as helpCmd).
func helpFlag(w io.Writer, flag, desc string) {
	helpCmd(w, flag, desc)
}

// helpUsage prints the dim "usage:" line + cyan binary name.
func helpUsage(w io.Writer, synopsis string) {
	fmt.Fprintln(w, dim("usage:"))
	fmt.Fprintf(w, "  %s\n", cyan(synopsis))
	fmt.Fprintln(w)
}

func printKV(w io.Writer, label, value string) {
	fmt.Fprintf(w, "  %s  %s\n", dim(padRight(label, 9)), value)
}

// printCostBlock colorizes a multi-line cost estimate (title yellow, $ green, assumptions dim).
func printCostBlock(w io.Writer, block string) {
	for i, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case i == 0:
			fmt.Fprintln(w, boldYellow(line))
		case trimmed == "assumptions" || strings.HasPrefix(trimmed, "·"):
			fmt.Fprintln(w, dim(line))
		case strings.HasPrefix(trimmed, "daily scenarios"):
			fmt.Fprintln(w, cyan(line))
		default:
			fmt.Fprintln(w, highlightUSD(line))
		}
	}
}

var usdRe = regexp.MustCompile(`\$[0-9]+(?:\.[0-9]+)?`)

func highlightUSD(s string) string {
	if !useColor() || !strings.Contains(s, "$") {
		return s
	}
	return usdRe.ReplaceAllStringFunc(s, green)
}

func stockLabel(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "HIGH", "MEDIUM", "LOW", "NONE", "SECURE", "COMMUNITY":
		u := strings.ToUpper(strings.TrimSpace(s))
		switch u {
		case "HIGH":
			return green(s)
		case "MEDIUM":
			return yellow(s)
		case "LOW", "NONE":
			return red(s)
		default:
			return cyan(s)
		}
	default:
		return s
	}
}

func commands(w io.Writer, title string, lines ...string) {
	if title != "" {
		fmt.Fprintln(w, title)
	}
	for _, line := range lines {
		if line == "" {
			fmt.Fprintln(w)
			continue
		}
		fmt.Fprintln(w, "  "+cyan(line))
	}
	fmt.Fprintln(w)
}

func hubLink(id string) string {
	return "https://huggingface.co/" + id
}

// osc8 wraps text in an OSC 8 hyperlink when stdout is a color TTY.
func osc8(url, text string) string {
	if url == "" || text == "" || !useColor() {
		return text
	}
	esc := string(rune(0x1b))
	return esc + "]8;;" + url + esc + "\\" + text + esc + "]8;;" + esc + "\\"
}

func actionsCell(id string) string {
	url := hubLink(id)
	// 🔗 opens Hub via OSC-8 hyperlink. 📋 is plain text (copy via --copy N or interactive 'copy N').
	return osc8(url, "🔗") + " 📋"
}

func padRight(s string, n int) string {
	if n <= 0 {
		return s
	}
	w := utf8.RuneCountInString(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

func truncateRunes(s string, n int) string {
	if n <= 0 {
		return s
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// clampWrapWidth returns 0 (ellipsis truncate) or a wrap width in [12, 80].
func clampWrapWidth(n int) int {
	if n <= 0 {
		return 0
	}
	if n < 12 {
		return 12
	}
	if n > 80 {
		return 80
	}
	return n
}

// resolveWrapWidth picks the largest positive wrap width from flag values.
func resolveWrapWidth(vals ...int) int {
	n := 0
	for _, v := range vals {
		if v > n {
			n = v
		}
	}
	return clampWrapWidth(n)
}
