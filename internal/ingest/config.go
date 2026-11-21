package ingest

import (
	"os"
	"strconv"
)

// Config controls ingest worker limits (env INGEST_*).
type Config struct {
	ChunkChars int
	MaxBytes   int
	OCR        bool
}

func LoadConfig() Config {
	c := Config{ChunkChars: 500, MaxBytes: 5 << 20, OCR: false}
	if v := os.Getenv("INGEST_CHUNK_CHARS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.ChunkChars = n
		}
	}
	if v := os.Getenv("INGEST_MAX_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.MaxBytes = n
		}
	}
	if os.Getenv("INGEST_OCR") == "on" {
		c.OCR = true
	}
	return c
}
