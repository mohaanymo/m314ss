package subtitles

import (
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/mohaanymo/m314-sub/audio"
	"github.com/mohaanymo/m314-sub/srt"
	"github.com/mohaanymo/m314-sub/vad"
)

const (
	windowLen  = 180.0 // seconds of audio per sampled window
	numWindows = 10

	// below this it's not worth rewriting a good file
	minShift = 0.15
)

// UseFFmpeg points the decoder at a specific ffmpeg binary. Default is
// whatever is on PATH.
func UseFFmpeg(path string) { audio.FFmpeg = path }

// Speech is where a video talks: the moments dialogue starts, plus which parts
// of the runtime were actually decoded.
//
// Building one costs a few seconds of ffmpeg. Matching a subtitle against it
// is milliseconds, so build once and test as many candidates as you want.
type Speech struct {
	Onsets   []float64
	Windows  [][2]float64
	Duration float64
}

// SyncResult is what matching one subtitle concluded.
type SyncResult struct {
	Alignment
	Shifted bool // whether the timings were actually changed
}

// Listen decodes a spread of the video's audio and finds where speech starts.
func Listen(videoPath string) (*Speech, error) {
	duration, err := audio.Duration(videoPath)
	if err != nil {
		return nil, fmt.Errorf("probing video: %w", err)
	}
	windows := pickWindows(duration)

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		onsets []float64
		failed error
	)
	for _, w := range windows {
		wg.Add(1)
		go func(start, length float64) {
			defer wg.Done()
			raw, e := audio.ExtractWindow(videoPath, start, length)
			if e == nil {
				var segments []vad.SpeechSegment
				segments, e = vad.Detect(raw, audio.SampleRate)
				if e == nil {
					mu.Lock()
					defer mu.Unlock()
					for _, seg := range segments {
						onsets = append(onsets, start+seg.Start)
					}
					return
				}
			}
			mu.Lock()
			failed = e
			mu.Unlock()
		}(w[0], w[1])
	}
	wg.Wait()

	if failed != nil {
		return nil, fmt.Errorf("extracting audio: %w", failed)
	}
	if len(onsets) == 0 {
		return nil, fmt.Errorf("no speech found in %s", videoPath)
	}
	sort.Float64s(onsets)
	return &Speech{Onsets: onsets, Windows: windows, Duration: duration}, nil
}

// Match tests one subtitle against the video and returns it with the timing
// correction applied.
//
// A subtitle that doesn't belong comes back unchanged with Matched false.
// Shifting a wrong subtitle just turns an obviously wrong one into a subtly
// wrong one.
func (s *Speech) Match(data []byte) (SyncResult, []byte, error) {
	cues := srt.Parse(data)
	if len(cues) == 0 {
		return SyncResult{}, data, fmt.Errorf("no cues found (not an SRT?)")
	}
	a := Align(srt.Starts(cues), s.Onsets, s.Windows)
	res := SyncResult{Alignment: a}
	if !a.Matched() || math.Abs(a.Offset) < minShift {
		return res, data, nil
	}
	res.Shifted = true
	return res, srt.Shift(data, a.Offset), nil
}

// Sync is Listen followed by Match, for when you only have one subtitle.
func Sync(videoPath string, data []byte) (SyncResult, []byte, error) {
	speech, err := Listen(videoPath)
	if err != nil {
		return SyncResult{}, data, err
	}
	return speech.Match(data)
}

// pickWindows spreads sample windows across the runtime, skipping the very
// start and end (intros and credits, usually no dialogue).
func pickWindows(duration float64) [][2]float64 {
	margin := duration * 0.05
	usable := duration - 2*margin

	if usable <= windowLen*numWindows {
		return [][2]float64{{0, duration}} // short enough, decode it all
	}

	windows := make([][2]float64, numWindows)
	stride := (usable - windowLen) / float64(numWindows-1)
	for i := range windows {
		windows[i] = [2]float64{margin + float64(i)*stride, windowLen}
	}
	return windows
}
