package subtitles

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// opensubtitles candidates must not carry an ID. the translation fallback
// would hand it to subdl, which wants its own nId.
func TestOpenSubtitlesCandidatesCarryNoTranslationID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{
			"attributes": map[string]any{
				"release": "Show.S01E01.WEB", "download_count": 900,
				"files": []map[string]any{{"file_id": 42}},
			}}}})
	}))
	defer srv.Close()

	o := &OpenSubtitles{APIKey: "k", HTTP: srv.Client()}
	cands, err := o.findAt(context.Background(), srv.URL, Query{TMDBID: 1396, Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want 1", len(cands))
	}
	if cands[0].ID != "" {
		t.Errorf("ID = %q, want empty", cands[0].ID)
	}
	if cands[0].Source != "opensubtitles" || cands[0].Release != "Show.S01E01.WEB" {
		t.Errorf("got %+v", cands[0])
	}
}

func TestOpenSubtitlesRanksByDownloads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mk := func(rel string, n, id int) map[string]any {
			return map[string]any{"attributes": map[string]any{
				"release": rel, "download_count": n,
				"files": []map[string]any{{"file_id": id}}}}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{mk("unpopular", 5, 1), mk("popular", 5000, 2)}})
	}))
	defer srv.Close()

	o := &OpenSubtitles{APIKey: "k", HTTP: srv.Client()}
	cands, _ := o.findAt(context.Background(), srv.URL, Query{TMDBID: 1})
	if len(cands) != 2 || cands[0].Release != "popular" {
		t.Errorf("wrong order: %+v", cands)
	}
}

// empty lang means any. sending languages= empty returns nothing from the
// real API.
func TestOpenSubtitlesOmitsEmptyLanguage(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	o := &OpenSubtitles{APIKey: "k", HTTP: srv.Client()}
	o.findAt(context.Background(), srv.URL, Query{TMDBID: 1, Lang: ""})
	if strings.Contains(gotQuery, "languages=") {
		t.Errorf("query %q sends an empty languages filter", gotQuery)
	}
	o.findAt(context.Background(), srv.URL, Query{TMDBID: 1, Lang: "AR"})
	if !strings.Contains(gotQuery, "languages=ar") {
		t.Errorf("query %q should lowercase the language", gotQuery)
	}
}
