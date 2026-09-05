package subtitles

import (
	"archive/zip"
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// SubDL searches subdl.com.
//
// Goes first because downloads aren't metered on the free tier and one
// download usually gives you a whole season. The catalogue has holes on niche
// and older titles, which is what the OpenSubtitles fallback is for.
//
// A pro key is supposed to unlock the AI endpoints in ai.go, but that API
// isn't deployed yet. See the note there.
type SubDL struct {
	APIKey string
	HTTP   *http.Client
}

func (s *SubDL) Name() string { return "subdl" }

const subdlAPI = "https://api.subdl.com/api/v1/subtitles"
const subdlDL = "https://dl.subdl.com"

type subdlResponse struct {
	Status    bool   `json:"status"`
	Error     string `json:"error"`
	Message   string `json:"message"`
	Subtitles []struct {
		ReleaseName string `json:"release_name"`
		Name        string `json:"name"`
		NID         string `json:"nId"`
		// "/s/info/<token>". The live v1 API never actually sends nId, so
		// this token is the only per-subtitle handle we get and the best
		// guess for the n_id the translation endpoint wants. Can't verify
		// until v2 is up.
		SubtitlePage string `json:"subtitlePage"`
		URL          string `json:"url"`
		Season       int    `json:"season"`
		Episode      int    `json:"episode"`
		FullSeason   bool   `json:"full_season"`
	} `json:"subtitles"`
}

func (s *SubDL) Find(ctx context.Context, q Query) ([]Candidate, error) {
	p := url.Values{
		"api_key":       {s.APIKey},
		"subs_per_page": {"30"},
	}
	// empty lang = any. leaving the param off is how subdl says that,
	// sending it empty returns nothing.
	if q.Lang != "" {
		p.Set("languages", strings.ToUpper(q.Lang))
	}
	switch {
	case q.TMDBID != 0:
		p.Set("tmdb_id", strconv.Itoa(q.TMDBID))
	case q.IMDBID != "":
		p.Set("imdb_id", q.IMDBID)
	default:
		p.Set("film_name", q.Title)
		if q.Year != 0 {
			p.Set("year", strconv.Itoa(q.Year))
		}
	}
	if q.isEpisode() {
		p.Set("type", "tv")
		p.Set("season_number", strconv.Itoa(q.Season))
		p.Set("episode_number", strconv.Itoa(q.Episode))
	} else {
		p.Set("type", "movie")
	}

	var body subdlResponse
	if err := getJSON(ctx, s.HTTP, subdlAPI+"?"+p.Encode(), nil, &body); err != nil {
		return nil, err
	}
	if !body.Status {
		msg := body.Message
		if msg == "" {
			msg = body.Error
		}
		return nil, fmt.Errorf("subdl: %s", msg)
	}

	out := make([]Candidate, 0, len(body.Subtitles))
	for _, sub := range body.Subtitles {
		href := sub.URL
		if href == "" {
			continue
		}
		name := sub.ReleaseName
		if name == "" {
			name = sub.Name
		}
		out = append(out, Candidate{
			Source:  s.Name(),
			Release: name,
			ID:      cmp.Or(sub.NID, pageToken(sub.SubtitlePage)),
			Fetch: func(ctx context.Context) ([]byte, error) {
				return s.download(ctx, href, q)
			},
		})
	}
	return out, nil
}

// download pulls the archive and picks the member we want.
//
// It's a zip even when the API says full_season:false (one "single episode"
// download came back with seven episodes in it), so the filenames inside the
// archive are the only thing worth trusting.
func (s *SubDL) download(ctx context.Context, href string, q Query) ([]byte, error) {
	u := href
	if !strings.HasPrefix(u, "http") {
		u = subdlDL + "/" + strings.TrimPrefix(href, "/")
	}
	if !strings.Contains(u, "api_key=") {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + "api_key=" + url.QueryEscape(s.APIKey)
	}

	raw, err := getBytes(ctx, s.HTTP, u, nil)
	if err != nil {
		return nil, err
	}
	// some entries are a bare .srt
	if !bytes.HasPrefix(raw, []byte("PK")) {
		return raw, nil
	}

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("subdl: unreadable archive: %w", err)
	}

	var subs []*zip.File
	var names []string
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.EqualFold(path.Ext(f.Name), ".srt") {
			continue
		}
		subs = append(subs, f)
		names = append(names, f.Name)
	}
	if len(subs) == 0 {
		return nil, fmt.Errorf("subdl: archive holds no .srt (%d entries)", len(zr.File))
	}
	if !q.isEpisode() {
		return readZipFile(subs[0])
	}
	if i := pickEpisode(names, q.Season, q.Episode); i >= 0 {
		return readZipFile(subs[i])
	}
	return nil, fmt.Errorf("subdl: archive has no S%02dE%02d (holds %d: %s)",
		q.Season, q.Episode, len(names), strings.Join(names[:min(3, len(names))], ", "))
}

// pageToken pulls the id out of a "/s/info/<token>" path.
func pageToken(page string) string {
	page = strings.TrimSuffix(page, "/")
	if i := strings.LastIndex(page, "/"); i >= 0 {
		return page[i+1:]
	}
	return page
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
