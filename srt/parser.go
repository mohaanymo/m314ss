// Package srt reads and retimes SubRip subtitles.
//
// Only the timestamps are parsed. Cue text passes through untouched, so
// encoding doesn't matter.
package srt

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Cue is one subtitle interval, in seconds from the start of the video.
type Cue struct {
	Start float64
	End   float64
}

var tsRe = regexp.MustCompile(`(\d+):(\d{2}):(\d{2})[,.](\d{3})`)

// Parse extracts cue timings. Malformed lines are skipped, not fatal.
// Subtitles in the wild are full of small breakage.
func Parse(data []byte) []Cue {
	var cues []Cue
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "-->") {
			continue
		}
		parts := strings.Split(line, "-->")
		if len(parts) != 2 {
			continue
		}
		s, ok1 := parseTime(parts[0])
		e, ok2 := parseTime(parts[1])
		if ok1 && ok2 {
			cues = append(cues, Cue{Start: s, End: e})
		}
	}
	return cues
}

// Starts returns just the cue start times.
func Starts(cues []Cue) []float64 {
	out := make([]float64, len(cues))
	for i, c := range cues {
		out[i] = c.Start
	}
	return out
}

// Shift moves every timestamp by seconds, leaving text alone. Negative
// results clamp to zero.
func Shift(data []byte, seconds float64) []byte {
	return tsRe.ReplaceAllFunc(data, func(m []byte) []byte {
		t, _ := parseTime(string(m))
		if t += seconds; t < 0 {
			t = 0
		}
		ms := int(t*1000 + 0.5)
		return fmt.Appendf(nil, "%02d:%02d:%02d,%03d",
			ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
	})
}

func parseTime(s string) (float64, bool) {
	m := tsRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	n := func(x string) float64 { v, _ := strconv.Atoi(x); return float64(v) }
	return n(m[1])*3600 + n(m[2])*60 + n(m[3]) + n(m[4])/1000, true
}
