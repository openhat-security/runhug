package gpudb

import (
	"regexp"
	"strings"
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, p := range []string{"nvidia ", "geforce ", "tesla ", "quadro "} {
		s = strings.ReplaceAll(s, p, "")
	}
	return nonAlnum.ReplaceAllString(s, "")
}

func tokenOverlap(a, b string) int {
	if len(a) < 3 || len(b) < 3 {
		return 0
	}
	score := 0
	for _, tok := range distinctiveTokens(a) {
		if len(tok) >= 3 && strings.Contains(b, tok) {
			score += len(tok)
		}
	}
	return score
}

func distinctiveTokens(n string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() >= 2 {
			out = append(out, cur.String())
		}
		cur.Reset()
	}
	for _, r := range n {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}
