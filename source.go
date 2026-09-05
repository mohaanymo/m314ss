package subtitles

import (
	"context"
	"errors"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Query says what we want a subtitle for.
type Query struct {
	TMDBID  int    // series id for TV, movie id for film
	IMDBID  string // "tt1234567", used when TMDBID is 0
	Title   string // free text fallback when no id is known
	Year    int
	Season  int // 0 for a movie
	Episode int // 0 for a movie

	// Lang is the language wanted. Empty means any, which is what the
	// translation fallback searches with.
	Lang string
}

func (q Query) isEpisode() bool { return q.Season > 0 && q.Episode > 0 }

// Candidate is one subtitle a Source found. Fetch is deferred because
// downloading is the rate limited half; only pay for the ones we try.
type Candidate struct {
	Source  string
	Release string

	// The source's own id for this subtitle, if it has one. SubDL fills it
	// with the nId its translation endpoint takes.
	ID string

	Fetch func(context.Context) ([]byte, error)
}

// Source is one subtitle site.
type Source interface {
	Name() string
	Find(context.Context, Query) ([]Candidate, error)
}

// ErrNoMatch means every candidate was either unavailable or failed to align.
var ErrNoMatch = errors.New("no subtitle matched this video")

// S01E02, 1x02
var episodeRe = regexp.MustCompile(`(?i)(?:s(\d{1,2})[\s._-]*e(\d{1,3})|(\d{1,2})x(\d{1,3}))`)

var numberRe = regexp.MustCompile(`\d+`)

// pickEpisode returns the index of the archive member holding the episode we
// want, or -1.
//
// Season packs are the norm on subdl and uploaders name the members however
// they like: "Show.S01E02.srt" but just as often "show <arabic title> 2.srt".
func pickEpisode(names []string, season, episode int) int {
	for i, n := range names {
		if matchesEpisode(n, season, episode) {
			return i
		}
	}

	// Fall back to a bare number, but only where it can't mislead. If any
	// member has an explicit marker then this pack does label its episodes
	// and ours just isn't here; guessing at loose digits would pick a
	// neighbour. Same if a number matches two members ("season 1 ep 5" vs
	// "ep 1"). A wrong subtitle is worse than none.
	for _, n := range names {
		if episodeRe.MatchString(n) {
			return -1
		}
	}
	found := -1
	for i, n := range names {
		if !hasNumber(n, episode) {
			continue
		}
		if found >= 0 {
			return -1
		}
		found = i
	}
	return found
}

func matchesEpisode(name string, season, episode int) bool {
	for _, m := range episodeRe.FindAllStringSubmatch(name, -1) {
		s, e := m[1], m[2]
		if s == "" {
			s, e = m[3], m[4]
		}
		si, _ := strconv.Atoi(s)
		ei, _ := strconv.Atoi(e)
		if si == season && ei == episode {
			return true
		}
	}
	return false
}

// hasNumber reports whether any run of digits in the name equals n. Whole
// runs only, so "11" never matches 1.
func hasNumber(name string, n int) bool {
	name = strings.TrimSuffix(name, path.Ext(name))
	for _, d := range numberRe.FindAllString(name, -1) {
		if v, err := strconv.Atoi(d); err == nil && v == n {
			return true
		}
	}
	return false
}
