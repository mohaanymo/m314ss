package srt

import (
	"bytes"
	"testing"
)

func TestParseAndShift(t *testing.T) {
	in := []byte("1\r\n00:01:56,073 --> 00:01:58,474\r\nمرحبا\r\n\r\n2\n00:00:02.500 --> 00:00:03,000\nbroken --> line\n")
	cues := Parse(in)
	if len(cues) != 2 {
		t.Fatalf("got %d cues, want 2", len(cues))
	}
	if cues[0].Start != 116.073 {
		t.Fatalf("start = %v, want 116.073", cues[0].Start)
	}

	// shifting past zero clamps, doesn't wrap
	out := Shift(in, -200)
	if c := Parse(out); c[0].Start != 0 || c[1].Start != 0 {
		t.Fatalf("clamp failed: %+v", c)
	}
	if !bytes.Contains(out, []byte("مرحبا")) {
		t.Fatal("cue text was mangled")
	}
	if back := Parse(Shift(in, 10)); back[0].Start != 126.073 {
		t.Fatalf("shifted start = %v, want 126.073", back[0].Start)
	}
}
