package ingest

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

var (
	ErrEmptyText = errors.New("empty_text")
	ErrNeedsOCR  = errors.New("needs_ocr")
	ErrTooLarge  = errors.New("too_large")
	ErrUnsupported = errors.New("unsupported_type")
)

// ExtractText pulls UTF-8 text from plain text or a PDF with a text layer (FR-102/107).
func ExtractText(data []byte, filename string, maxBytes int, ocrEnabled bool) (string, error) {
	if maxBytes > 0 && len(data) > maxBytes {
		return "", fmt.Errorf("%w: %d > %d", ErrTooLarge, len(data), maxBytes)
	}
	name := strings.ToLower(filename)
	switch {
	case looksPlain(data, name):
		if !utf8.Valid(data) {
			return "", fmt.Errorf("%w: not utf-8", ErrEmptyText)
		}
		s := strings.TrimSpace(string(data))
		if s == "" {
			return "", ErrEmptyText
		}
		return s, nil
	case looksPDF(data, name):
		s, err := extractPDF(data)
		if err != nil {
			return "", err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			if ocrEnabled {
				return "", ErrNeedsOCR // Phase B not implemented
			}
			return "", ErrNeedsOCR
		}
		return s, nil
	default:
		return "", ErrUnsupported
	}
}

func looksPlain(data []byte, name string) bool {
	if strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".md") {
		return true
	}
	if looksPDF(data, name) {
		return false
	}
	// heuristic: mostly printable
	if len(data) == 0 {
		return false
	}
	return utf8.Valid(data) && !bytes.Contains(data[:min(8, len(data))], []byte("%PDF"))
}

func looksPDF(data []byte, name string) bool {
	if strings.HasSuffix(name, ".pdf") {
		return true
	}
	return bytes.HasPrefix(data, []byte("%PDF"))
}

func extractPDF(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNeedsOCR, err)
	}
	var b strings.Builder
	total := r.NumPage()
	for i := 1; i <= total; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		t, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		b.WriteString(t)
		if i < total {
			b.WriteString("\n\n")
		}
	}
	return b.String(), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
