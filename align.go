package subtitles

import (
	"fmt"
	"math"
	"sort"
	"sync"
)

const (
	// a cue start counts as a hit if a speech onset is within this many
	// seconds of it. wide enough for VAD jitter, narrow enough that random
	// cues miss.
	tolerance = 0.35
	step      = 0.05  // offset search step, seconds
	maxOffset = 120.0 // how far out of sync we bother looking
)

// Alignment is the result of matching one subtitle against one video.
type Alignment struct {
	// a cue at t belongs at t*Scale + Offset. Scale is 1 unless the
	// subtitle drifts, see frameRates.
	Scale  float64
	Offset float64

	// HitRate is the fraction of cue starts explained at Offset. Baseline and
	// StdDev are the mean and spread of that rate across every offset tried,
	// i.e. what this pairing scores by chance.
	HitRate  float64
	Baseline float64
	StdDev   float64
}

// Lift is how far the best offset beats chance, in percentage points. Nice
// to show a human, not enough to decide on. See Sigma.
func (a Alignment) Lift() float64 { return a.HitRate - a.Baseline }

// Sigma is how far the winning offset stands out from every other offset, in
// standard deviations. This is the number that decides a match.
//
// Raw hit rate doesn't work across titles: a clean live action film gives its
// own subtitle ~60%, fast overlapping dialogue over music gives its own
// subtitle ~29% because most cues start mid-run where there is no onset.
// Lift has the same problem, I've seen a subtitle for a totally different
// show score +14 against a video whose correct subtitle scored +9.
//
// What actually separates them is the shape of the search. A matching
// subtitle gives one narrow spike at the true offset; a wrong one gives a
// broad wobble that may happen to peak higher. Dividing by the spread
// measures exactly that and it self normalises, noisy audio raises the
// spread along with the peak.
func (a Alignment) Sigma() float64 {
	if a.StdDev <= 0 {
		return 0
	}
	return (a.HitRate - a.Baseline) / a.StdDev
}

// Correction is the fix in words, for logs: "+4.70s", or "+4.70s x1.042"
// when the subtitle also drifts.
func (a Alignment) Correction() string {
	if a.Scale == 1 {
		return fmt.Sprintf("%+.2fs", a.Offset)
	}
	return fmt.Sprintf("%+.2fs x%.4g", a.Offset, a.Scale)
}

// Matched reports whether the subtitle belongs to this video.
//
// Both gates are needed. Sigma alone let a film's subtitle through against a
// cartoon episode at 4.8 (lift +2.4). Lift alone let a different show through
// at +14.3 (sigma 3.1). Every wrong pairing I've measured fails at least one
// and every right one clears both.
func (a Alignment) Matched() bool { return a.Sigma() >= MinSigma && a.Lift() >= MinLift }

// Frame rates a subtitle may have been timed against. A file timed for a
// 25fps cut and played on a 24fps one starts fine and is 225s late by the
// end of a 90 minute film. No single offset fixes that, so Align also tries
// every ratio of these. 29.97 and 30 aren't here on purpose: getting to
// those from 24 is pulldown, which keeps the running time, so nothing
// drifts.
var frameRates = []float64{23.976, 24, 25}

// Align finds the scale and offset that best explain the subtitle's cue
// starts given the video's speech onsets.
//
// Scale 1 wins ties, a rate ratio has to give a strictly sharper spike. Each
// extra ratio is one more chance for a wrong subtitle to find a noise peak;
// the gate hasn't been re-measured with them in.
func Align(cues, onsets []float64, windows [][2]float64) Alignment {
	if len(cues) == 0 || len(onsets) == 0 {
		return Alignment{Scale: 1}
	}
	scales := []float64{1}
	for _, from := range frameRates {
		for _, to := range frameRates {
			if from != to {
				scales = append(scales, from/to)
			}
		}
	}
	// each slide is a quarter second on a two hour film, so run them side
	// by side
	found := make([]Alignment, len(scales))
	var wg sync.WaitGroup
	for i, s := range scales {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scaled := make([]float64, len(cues))
			for j, c := range cues {
				scaled[j] = c * s
			}
			found[i] = slide(scaled, onsets, windows)
			found[i].Scale = s
		}()
	}
	wg.Wait()
	best := found[0]
	for _, a := range found[1:] {
		if a.Sigma() > best.Sigma() {
			best = a
		}
	}
	return best
}

// slide finds the offset that best explains the cue starts.
//
// It compares onsets, not speech/silence masks. Masks fail on real material:
// with a continuous score the speech mask is ~80% true, every offset overlaps
// about equally and the winner is noise (got +4.6s once for a subtitle whose
// real offset was -110s). Onsets stay sparse however loud the film is.
//
// We only decode part of a long film, so a cue is only judged if its shifted
// time lands inside a window we actually looked at. windows are {start,
// length} pairs; pass nil if the whole thing was decoded.
//
// onsets must be sorted.
func slide(cues, onsets []float64, windows [][2]float64) Alignment {
	// don't trust a rate computed off a handful of cues
	minJudged := len(cues) / 10
	if minJudged < 20 {
		minJudged = 20
	}

	var out Alignment
	best, sum, sumSq, n := -1.0, 0.0, 0.0, 0.0
	for off := -maxOffset; off <= maxOffset; off += step {
		hits, judged := 0, 0
		for _, c := range cues {
			t := c + off
			if !covered(t, windows) {
				continue
			}
			judged++
			if i := sort.SearchFloat64s(onsets, t-tolerance); i < len(onsets) && onsets[i] <= t+tolerance {
				hits++
			}
		}
		if judged < minJudged {
			continue
		}
		rate := float64(hits) / float64(judged)
		if rate > best {
			best, out.Offset = rate, off
		}
		sum, sumSq, n = sum+rate, sumSq+rate*rate, n+1
	}
	if n == 0 {
		return Alignment{}
	}
	out.HitRate, out.Baseline = best, sum/n
	out.StdDev = math.Sqrt(math.Max(sumSq/n-out.Baseline*out.Baseline, 0))
	return out
}

func covered(t float64, windows [][2]float64) bool {
	if windows == nil {
		return true
	}
	for _, w := range windows {
		if t >= w[0] && t < w[0]+w[1] {
			return true
		}
	}
	return false
}

// MinSigma is the acceptance gate. Numbers from the pairings I tested:
//
//	                                          lift   sigma
//	film + own subtitle ..................... +40.5   17.6   ok
//	second film + own subtitle .............. +39.3   16.8   ok
//	cartoon + own season pack ............... +12.4    6.4   ok
//	cartoon + own episode subtitle ..........  +9.1    5.4   ok
//	film + other film's subtitle ............  +3.3    3.5   wrong
//	cartoon + different show's subtitle ..... +14.3    3.1   wrong
//	film + different film's subtitle ........  +5.5    3.1   wrong
//
// Right pairings sit at 5.4 and up. Wrong ones mostly at 3.5 and below, but a
// film subtitle against a cartoon episode reached 4.8, which is why MinLift
// exists (that one fails it at +2.4).
//
// 4.8 to 5.4 is the tightest margin in the set. Fast overlapping dialogue over
// music is the hard case: a correct subtitle only gets ~29% against ~21% by
// chance. Both gates are vars so they can be retuned.
var MinSigma = 5.0

// MinLift: the winning offset has to beat chance by at least this much. No
// real match has failed it (lowest seen +9.1) and it catches high sigma noise.
var MinLift = 0.05
