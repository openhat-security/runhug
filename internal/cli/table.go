package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// tableCol describes one column in printTable.
type tableCol struct {
	Title string
	Min   int
	Max   int  // 0 = unlimited
	Right bool // numeric / money
}

// tableCell is unstyled text plus an optional color wrapper applied after padding.
type tableCell struct {
	Text  string
	Style func(string) string
}

func cell(text string) tableCell {
	return tableCell{Text: text}
}

func styledCell(text string, style func(string) string) tableCell {
	return tableCell{Text: text, Style: style}
}

// printTable draws a padded column table (header + rows) with a 2-space indent.
func printTable(w io.Writer, cols []tableCol, rows [][]tableCell) {
	if w == nil || len(cols) == 0 {
		return
	}
	widths := make([]int, len(cols))
	for j, c := range cols {
		widths[j] = tableColWidth(c, rows, j)
	}
	printTableLine(w, cols, widths, headerCells(cols), true)
	for _, row := range rows {
		printTableLine(w, cols, widths, row, false)
	}
}

func headerCells(cols []tableCol) []tableCell {
	out := make([]tableCell, len(cols))
	for i, c := range cols {
		out[i] = tableCell{Text: c.Title, Style: dim}
	}
	return out
}

func tableColWidth(c tableCol, rows [][]tableCell, j int) int {
	w := utf8.RuneCountInString(c.Title)
	if w < c.Min {
		w = c.Min
	}
	for _, row := range rows {
		if j >= len(row) {
			continue
		}
		t := row[j].Text
		if c.Max > 0 {
			t = truncateRunes(t, c.Max)
		}
		if n := utf8.RuneCountInString(t); n > w {
			w = n
		}
	}
	if c.Max > 0 && w > c.Max {
		w = c.Max
	}
	return w
}

func printTableLine(w io.Writer, cols []tableCol, widths []int, row []tableCell, header bool) {
	parts := make([]string, 0, len(cols))
	for j, c := range cols {
		text := ""
		var style func(string) string
		if j < len(row) {
			text = row[j].Text
			style = row[j].Style
		}
		if c.Max > 0 {
			text = truncateRunes(text, widths[j])
		}
		padded := padRight(text, widths[j])
		if c.Right && !header {
			padded = padLeft(text, widths[j])
		}
		if style != nil {
			padded = style(padded)
		}
		parts = append(parts, padded)
	}
	fmt.Fprintln(w, "  "+strings.Join(parts, "  "))
}

func tableUSD(v float64) string {
	if v <= 0 {
		return "—"
	}
	s := fmt.Sprintf("$%.4f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if !strings.Contains(s, ".") {
		return s + ".00"
	}
	return s
}

func tableUSD2(v float64) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf("$%.2f", v)
}

func padLeft(s string, n int) string {
	if n <= 0 {
		return s
	}
	w := utf8.RuneCountInString(s)
	if w >= n {
		return s
	}
	return strings.Repeat(" ", n-w) + s
}
