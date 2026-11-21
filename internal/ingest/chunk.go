package ingest

import (
	"strings"
	"unicode/utf8"
)

// Chunk splits text into pieces of at most maxChars, preferring paragraph breaks.
func Chunk(text string, maxChars int) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxChars <= 0 {
		maxChars = 500
	}
	paras := strings.Split(text, "\n\n")
	var out []string
	var buf strings.Builder
	flush := func() {
		s := strings.TrimSpace(buf.String())
		if s != "" {
			out = append(out, s)
		}
		buf.Reset()
	}
	for _, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if buf.Len() == 0 {
			if utf8.RuneCountInString(p) <= maxChars {
				buf.WriteString(p)
				continue
			}
			out = append(out, splitLong(p, maxChars)...)
			continue
		}
		trial := buf.String() + "\n\n" + p
		if utf8.RuneCountInString(trial) <= maxChars {
			buf.WriteString("\n\n")
			buf.WriteString(p)
			continue
		}
		flush()
		if utf8.RuneCountInString(p) <= maxChars {
			buf.WriteString(p)
		} else {
			out = append(out, splitLong(p, maxChars)...)
		}
	}
	flush()
	return out
}

func splitLong(s string, maxChars int) []string {
	runes := []rune(s)
	var out []string
	for len(runes) > 0 {
		n := maxChars
		if n > len(runes) {
			n = len(runes)
		}
		// prefer break at space
		if n < len(runes) {
			for i := n; i > n*2/3; i-- {
				if runes[i] == ' ' || runes[i] == '\n' {
					n = i
					break
				}
			}
		}
		out = append(out, strings.TrimSpace(string(runes[:n])))
		runes = runes[n:]
		for len(runes) > 0 && (runes[0] == ' ' || runes[0] == '\n') {
			runes = runes[1:]
		}
	}
	return out
}
