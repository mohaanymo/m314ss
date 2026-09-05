package subtitles

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// SubDL pro AI endpoints: translate an existing subtitle, transcribe a media
// file, resolve a messy release filename to a title.
//
// These live on the v2 API and use a bearer token instead of the api_key
// query param v1 uses.
//
// NOT LIVE yet. Every /api/v2 path returns 404 NOT_FOUND with or without
// credentials while /api/v1 works with the same key. The API gives 403 for a
// bad key and 404 for an unknown route, so this is a missing router, not pro
// gating. subdl published the docs ahead of the deployment. Everything below
// follows the documented contract and should start working once they ship
// it, but none of it has been run against a live endpoint.
const subdlAPIv2 = "https://api.subdl.com/api/v2"

// Translation tones. Anything else is passed through as is.
const (
	ToneFaithful        = "faithful"
	ToneFormal          = "formal"
	ToneCasual          = "casual"
	ToneCinematicAction = "cinematic_action"
	ToneComedicFun      = "comedic_fun"
	ToneAnime           = "anime"
)

// Job is an async AI request, a translation or a transcription.
//
// Translations usually finish inside a minute. DownloadReady is the only field
// worth branching on; status strings are the server's vocabulary and may grow.
type Job struct {
	ID             string `json:"id"`
	RequestID      string `json:"request_id"`
	Status         string `json:"status"`
	Progress       int    `json:"progress"`
	ETA            int    `json:"eta"` // seconds
	DownloadReady  bool   `json:"download_ready"`
	DownloadURL    string `json:"download_url"`
	TargetLanguage string `json:"target_language"`
	Tone           string `json:"tone"`
	Error          string `json:"error"`
}

// Ref is the id to poll with. The create call answers with request_id and the
// status call with id, either may be the populated one.
func (j Job) Ref() string { return cmp.Or(j.RequestID, j.ID) }

// FilenameMatch is what the AI made of a release filename.
//
// The endpoint is beta and the response shape isn't pinned down, so Raw keeps
// the whole payload.
type FilenameMatch struct {
	Type    string `json:"type"` // "movie" or "tv"
	TMDBID  int    `json:"tmdb_id"`
	IMDBID  string `json:"imdb_id"`
	Title   string `json:"title"`
	Year    int    `json:"year"`
	Season  int    `json:"season"`
	Episode int    `json:"episode"`

	Raw json.RawMessage `json:"-"`
}

// Query turns a match into a subtitle search for the given language.
func (m FilenameMatch) Query(lang string) Query {
	return Query{
		TMDBID: m.TMDBID, IMDBID: m.IMDBID, Title: m.Title, Year: m.Year,
		Season: m.Season, Episode: m.Episode, Lang: lang,
	}
}

// Translate queues a translation of an existing subtitle into targetLang
// (e.g. "FA"). tone is optional, see the Tone constants. Returns as soon as
// the job is queued; use WaitTranslation or Translation to follow it.
func (s *SubDL) Translate(ctx context.Context, nID, targetLang, tone string) (Job, error) {
	if nID == "" || targetLang == "" {
		return Job{}, fmt.Errorf("subdl: translate needs an n_id and a target language")
	}
	body := map[string]string{"n_id": nID, "target_language": targetLang}
	if tone != "" {
		body["tone"] = tone
	}
	var job Job
	err := s.aiPost(ctx, "/ai/translations", body, &job)
	return job, err
}

// Translation reports the progress of one job.
func (s *SubDL) Translation(ctx context.Context, id string) (Job, error) {
	var job Job
	err := s.aiGet(ctx, "/ai/translations/"+url.PathEscape(id), &job)
	return job, err
}

// Translations lists your jobs. Pages start at 1, 0 means the first.
func (s *SubDL) Translations(ctx context.Context, page int) ([]Job, error) {
	p := "/ai/translations"
	if page > 0 {
		p += "?page=" + strconv.Itoa(page)
	}
	var jobs []Job
	err := s.aiGet(ctx, p, &jobs)
	return jobs, err
}

// WaitTranslation polls until the job is downloadable. every defaults to 5s.
// No built in deadline, cancel ctx to give up.
func (s *SubDL) WaitTranslation(ctx context.Context, id string, every time.Duration) (Job, error) {
	if every <= 0 {
		every = 5 * time.Second
	}
	for {
		job, err := s.Translation(ctx, id)
		if err != nil {
			return job, err
		}
		switch {
		case job.DownloadReady:
			return job, nil
		case job.Error != "":
			return job, fmt.Errorf("subdl: translation %s failed: %s", id, job.Error)
		case job.Status == "failed" || job.Status == "error":
			return job, fmt.Errorf("subdl: translation %s failed", id)
		}
		select {
		case <-ctx.Done():
			return job, ctx.Err()
		case <-time.After(every):
		}
	}
}

// DownloadTranslation fetches the finished subtitle. Job must be ready.
func (s *SubDL) DownloadTranslation(ctx context.Context, job Job) ([]byte, error) {
	if job.DownloadURL == "" {
		return nil, fmt.Errorf("subdl: translation %s has no download yet (status %q)", job.Ref(), job.Status)
	}
	return getBytes(ctx, s.HTTP, job.DownloadURL, s.aiHeaders())
}

// Transcribe generates subtitles from a media file. mediaURL has to be
// reachable by subdl, not a local path. lang is the spoken language. Beta on
// their side.
func (s *SubDL) Transcribe(ctx context.Context, mediaURL, lang string) (Job, error) {
	if mediaURL == "" {
		return Job{}, fmt.Errorf("subdl: transcribe needs a media_url")
	}
	body := map[string]string{"media_url": mediaURL}
	if lang != "" {
		body["language"] = lang
	}
	var job Job
	err := s.aiPost(ctx, "/ai/transcriptions", body, &job)
	return job, err
}

// FilenameSearch resolves a release filename to its best TMDB match. Beta.
func (s *SubDL) FilenameSearch(ctx context.Context, filename string) (FilenameMatch, error) {
	if filename == "" {
		return FilenameMatch{}, fmt.Errorf("subdl: filename search needs a filename")
	}
	raw, err := s.aiRaw(ctx, http.MethodGet, "/ai/filename-search?filename="+url.QueryEscape(filename), nil)
	if err != nil {
		return FilenameMatch{}, err
	}
	var m FilenameMatch
	if err := json.Unmarshal(raw, &m); err != nil {
		return FilenameMatch{}, fmt.Errorf("subdl: %w (%s)", err, snippet(raw))
	}
	m.Raw = raw
	return m, nil
}

func (s *SubDL) aiHeaders() http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+s.APIKey)
	h.Set("Accept", "application/json")
	return h
}

func (s *SubDL) aiGet(ctx context.Context, path string, out any) error {
	raw, err := s.aiRaw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("subdl: %w (%s)", err, snippet(raw))
	}
	return nil
}

func (s *SubDL) aiPost(ctx context.Context, path string, in, out any) error {
	raw, err := s.aiRaw(ctx, http.MethodPost, path, in)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("subdl: %w (%s)", err, snippet(raw))
	}
	return nil
}

func (s *SubDL) aiRaw(ctx context.Context, method, path string, in any) (json.RawMessage, error) {
	if s.APIKey == "" {
		return nil, fmt.Errorf("subdl: no API key")
	}
	var (
		raw []byte
		err error
	)
	if method == http.MethodPost {
		raw, err = postBytes(ctx, s.HTTP, subdlAPIv2+path, s.aiHeaders(), in)
	} else {
		raw, err = getBytes(ctx, s.HTTP, subdlAPIv2+path, s.aiHeaders())
	}
	if err != nil {
		return nil, err
	}
	return aiPayload(raw)
}

// aiPayload strips whatever envelope came back. The v2 docs show requests but
// not responses, so accept a bare object, a top level array, or the real
// answer wrapped in "data"/"result". Also turns a refusal delivered with a
// 200 into an error.
func aiPayload(raw []byte) (json.RawMessage, error) {
	for i, b := range raw {
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			continue
		}
		if b == '[' {
			return raw[i:], nil
		}
		break
	}

	var env struct {
		Status  *bool           `json:"status"`
		Success *bool           `json:"success"`
		Error   string          `json:"error"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("subdl: %w (%s)", err, snippet(raw))
	}
	failed := (env.Status != nil && !*env.Status) || (env.Success != nil && !*env.Success) || env.Error != ""
	if failed {
		return nil, fmt.Errorf("subdl: %s", cmp.Or(env.Error, env.Message, "request failed"))
	}
	if len(env.Data) > 0 && string(env.Data) != "null" {
		return env.Data, nil
	}
	if len(env.Result) > 0 && string(env.Result) != "null" {
		return env.Result, nil
	}
	return raw, nil
}
