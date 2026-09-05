package subtitles

import "testing"

func TestPickEpisode(t *testing.T) {
	// real member names from a subdl pack. bare episode numbers, arabic
	// parts mangled because the zip filenames aren't utf-8
	bare := []string{
		"we baby bears ???? 1 ??????.srt",
		"we  baby bears ???? 3 ??????.srt",
		"we baby bears ???? 10 ??????.srt",
		"we baby bears ???? 11 ??????.srt",
	}
	labelled := []string{
		"Breaking Bad s01e01 720p.BRrip.srt",
		"Breaking Bad s01e02 720p.BRrip.srt",
		"Breaking Bad s01e03 720p.BRrip.srt",
	}

	for _, tc := range []struct {
		name             string
		names            []string
		season, ep, want int
	}{
		{"bare number", bare, 1, 1, 0},
		{"bare number, two digits", bare, 1, 11, 3},
		{"10 must not match 1", bare, 1, 10, 2},
		{"absent episode", bare, 1, 7, -1},
		{"explicit label", labelled, 1, 2, 1},
		{"explicit label, wrong season", labelled, 2, 2, -1},
		// labelled pack without our episode must not fall through to loose
		// digits, "720" and "01" are everywhere
		{"labelled pack, episode absent", labelled, 1, 9, -1},
	} {
		if got := pickEpisode(tc.names, tc.season, tc.ep); got != tc.want {
			t.Errorf("%s: pickEpisode(S%02dE%02d) = %d, want %d", tc.name, tc.season, tc.ep, got, tc.want)
		}
	}

	ambiguous := []string{"show season 1 ep 5.srt", "show ep 1.srt"}
	if got := pickEpisode(ambiguous, 1, 1); got != -1 {
		t.Errorf("ambiguous names resolved to %d, want -1", got)
	}
}
