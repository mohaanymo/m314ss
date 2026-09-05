package vad

import (
	"math"
	"slices"
)

const (
	chunkMs = 30 // energy frame size

	// Speech threshold sits between the quiet floor and the loud peaks of
	// this window, not at a fixed dB. A whispered scene and an action scene
	// have completely different absolute levels.
	floorPct = 15
	loudPct  = 90
	mix      = 0.45 // tried 0.25, sparser onsets align better
)

// MergeGap joins speech segments closer than this many seconds. Exported so
// it can be swept.
var MergeGap = 0.3

type SpeechSegment struct {
	Start float64
	End   float64
}

// Detect finds speech in raw s16le PCM using frame energy against an adaptive
// threshold.
//
// This is not a real voice/music classifier and doesn't try to be. All it
// needs to produce is a timeline good enough to correlate against subtitle
// cue times, and that correlation runs over ~30 minutes of sampled audio so
// it shrugs off plenty of per frame mistakes. A proper VAD (webrtcvad) needs
// cgo and that would cost the cross compile.
//
// Energy only means loud music counts as speech. Got the same offsets as
// webrtcvad on the film I tested with, so leaving it. Revisit if something
// with a wall to wall score comes out misaligned.
func Detect(raw []byte, sampleRate int) ([]SpeechSegment, error) {
	frame := sampleRate * chunkMs / 1000 // samples per frame
	n := len(raw) / 2 / frame
	if n == 0 {
		return nil, nil
	}

	db := make([]float64, n)
	for i := range db {
		var sum float64
		for j := i * frame; j < (i+1)*frame; j++ {
			s := float64(int16(uint16(raw[2*j]) | uint16(raw[2*j+1])<<8))
			sum += s * s
		}
		db[i] = 20 * math.Log10(math.Sqrt(sum/float64(frame))+1e-9)
	}

	sorted := slices.Clone(db)
	slices.Sort(sorted)
	floor := sorted[len(sorted)*floorPct/100]
	loud := sorted[len(sorted)*loudPct/100]
	thr := floor + mix*(loud-floor)

	frameDur := float64(chunkMs) / 1000
	var segments []SpeechSegment
	inSpeech := false
	startTime := 0.0

	for i, e := range db {
		active := e > thr
		t := float64(i) * frameDur
		if active && !inSpeech {
			inSpeech, startTime = true, t
		} else if !active && inSpeech {
			inSpeech = false
			segments = append(segments, SpeechSegment{startTime, t})
		}
	}
	if inSpeech {
		segments = append(segments, SpeechSegment{startTime, float64(n) * frameDur})
	}

	return mergeClose(segments, MergeGap), nil
}

// mergeClose joins segments separated by less than maxGap seconds. Short
// pauses between words are the same utterance and subtitle cues span them.
func mergeClose(segments []SpeechSegment, maxGap float64) []SpeechSegment {
	if len(segments) == 0 {
		return segments
	}
	merged := segments[:1]
	for _, seg := range segments[1:] {
		last := &merged[len(merged)-1]
		if seg.Start-last.End < maxGap {
			last.End = seg.End
		} else {
			merged = append(merged, seg)
		}
	}
	return merged
}
