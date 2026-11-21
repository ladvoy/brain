package ingest

import "testing"

func TestChunkParagraphs(t *testing.T) {
	text := "First paragraph is short.\n\nSecond paragraph is also fine.\n\nThird."
	got := Chunk(text, 80)
	if len(got) < 1 {
		t.Fatal("expected chunks")
	}
	for _, c := range got {
		if c == "" {
			t.Fatal("empty chunk")
		}
	}
}

func TestChunkLongLine(t *testing.T) {
	var b []byte
	for i := 0; i < 200; i++ {
		b = append(b, []byte("word ")...)
	}
	got := Chunk(string(b), 40)
	if len(got) < 2 {
		t.Fatalf("expected split, got %d", len(got))
	}
}
