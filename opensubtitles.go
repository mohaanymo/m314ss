package subtitles

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// OpenSubtitles searches opensubtitles.com.
//
// Fallback rather than primary: downloads are capped (20/day free, 1000 on
// VIP) and each one costs a unit. But it covers the niche and older titles
// subdl doesn't have.
type OpenSubtitles struct {
	APIKey   string
	Username string
	Password string
	HTTP     *http.Client

	mu    sync.Mutex
	token string
}

func (o *OpenSubtitles) Name() string { return "opensubtitles" }

const osAPI = "https://api.opensubtitles.com/api/v1"

func (o *OpenSubtitles) headers() http.Header {
	h := http.Header{}
	h.Set("Api-Key", o.APIKey)
	h.Set("Accept", "application/json")
	h.Set("Content-Type", "application/json")
	// the API rejects requests without a client string
	h.Set("User-Agent", "m314ss/1.0")
	return h
}

// auth logs in once and caches the token.
func (o *OpenSubtitles) auth(ctx context.Context) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.token != "" {
		return o.token, nil
	}
	req := map[string]string{"username": o.Username, "password": o.Password}
	data, err := postBytes(ctx, o.HTTP, osAPI+"/login", o.headers(), req)
	if err != nil {
		return "", err
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return "", fmt.Errorf("opensubtitles: %w (%s)", err, snippet(data))
	}
	if body.Token == "" {
		return "", fmt.Errorf("opensubtitles: login returned no token")
	}
	o.token = body.Token
	return o.token, nil
}

func (o *OpenSubtitles) Find(ctx context.Context, q Query) ([]Candidate, error) {
	return o.findAt(ctx, osAPI, q)
}

// findAt is Find against a given base URL so tests can point it at a stub.
func (o *OpenSubtitles) findAt(ctx context.Context, base string, q Query) ([]Candidate, error) {
	p := url.Values{}
	if q.Lang != "" {
		p.Set("languages", strings.ToLower(q.Lang))
	}
	switch {
	case q.TMDBID != 0:
		p.Set("tmdb_id", strconv.Itoa(q.TMDBID))
	case q.IMDBID != "":
		p.Set("imdb_id", strings.TrimPrefix(q.IMDBID, "tt"))
	default:
		p.Set("query", q.Title)
	}
	if q.isEpisode() {
		p.Set("season_number", strconv.Itoa(q.Season))
		p.Set("episode_number", strconv.Itoa(q.Episode))
	}

	var body struct {
		Data []struct {
			Attributes struct {
				Release       string `json:"release"`
				DownloadCount int    `json:"download_count"`
				Files         []struct {
					FileID int `json:"file_id"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := getJSON(ctx, o.HTTP, base+"/subtitles?"+p.Encode(), o.headers(), &body); err != nil {
		return nil, err
	}

	// Most downloaded first. Best signal available and still a weak one
	// (the top S01E01 result for one show was actually episode 2), which is
	// why everything gets verified against the audio anyway.
	sort.SliceStable(body.Data, func(i, j int) bool {
		return body.Data[i].Attributes.DownloadCount > body.Data[j].Attributes.DownloadCount
	})

	var out []Candidate
	for _, d := range body.Data {
		for _, f := range d.Attributes.Files {
			if f.FileID == 0 {
				continue
			}
			id := f.FileID
			out = append(out, Candidate{
				Source:  o.Name(),
				Release: d.Attributes.Release,
				// ID left empty on purpose. It's for subdl's translator,
				// which wants a subdl nId; an opensubtitles file id would
				// just be a guaranteed failed request.
				Fetch: func(ctx context.Context) ([]byte, error) {
					return o.download(ctx, id)
				},
			})
		}
	}
	return out, nil
}

// download spends one of the day's allowance.
func (o *OpenSubtitles) download(ctx context.Context, fileID int) ([]byte, error) {
	token, err := o.auth(ctx)
	if err != nil {
		return nil, err
	}
	h := o.headers()
	h.Set("Authorization", "Bearer "+token)

	// the aligner only understands srt timestamps
	req := map[string]any{"file_id": fileID, "sub_format": "srt"}
	data, err := postBytes(ctx, o.HTTP, osAPI+"/download", h, req)
	if err != nil {
		return nil, err
	}
	var body struct {
		Link      string `json:"link"`
		Remaining int    `json:"remaining"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("opensubtitles: %w (%s)", err, snippet(data))
	}
	if body.Link == "" {
		msg := body.Message
		if msg == "" {
			msg = "no link returned (daily download limit reached?)"
		}
		return nil, fmt.Errorf("opensubtitles: %s", msg)
	}
	return getBytes(ctx, o.HTTP, body.Link, nil)
}
