package subtitles

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAIPayload(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"bare object", `{"request_id":"req_1"}`, `{"request_id":"req_1"}`},
		{"wrapped in data", `{"status":true,"data":{"request_id":"req_1"}}`, `{"request_id":"req_1"}`},
		{"wrapped in result", `{"result":{"request_id":"req_1"}}`, `{"request_id":"req_1"}`},
		{"top-level array", `[{"id":"a"}]`, `[{"id":"a"}]`},
		{"leading space, array", "  \n[{\"id\":\"a\"}]", `[{"id":"a"}]`},
		{"null data", `{"request_id":"req_1","data":null}`, `{"request_id":"req_1","data":null}`},
	} {
		got, err := aiPayload([]byte(tc.body))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}

	// a refusal with a 200 still has to be an error, and keep the server's
	// message (only clue that a pro endpoint got hit with a free key)
	for _, body := range []string{
		`{"status":false,"error":"subscription required"}`,
		`{"success":false,"message":"subscription required"}`,
	} {
		_, err := aiPayload([]byte(body))
		if err == nil {
			t.Fatalf("%s: no error", body)
		}
		if !strings.Contains(err.Error(), "subscription required") {
			t.Errorf("%s: lost the server message: %v", body, err)
		}
	}

	if _, err := aiPayload([]byte("<html>gateway timeout</html>")); err == nil {
		t.Error("non-JSON body accepted")
	}
}

func TestJobRef(t *testing.T) {
	if got := (Job{RequestID: "req_1"}).Ref(); got != "req_1" {
		t.Errorf("request_id: %q", got)
	}
	if got := (Job{ID: "id_1"}).Ref(); got != "id_1" {
		t.Errorf("id: %q", got)
	}
}

func TestFilenameMatchQuery(t *testing.T) {
	var m FilenameMatch
	json.Unmarshal([]byte(`{"type":"tv","tmdb_id":1396,"title":"Breaking Bad","season":1,"episode":2}`), &m)
	q := m.Query("ar")
	if q.TMDBID != 1396 || q.Lang != "ar" || !q.isEpisode() {
		t.Errorf("got %+v", q)
	}
}

// real values from api.subdl.com
func TestPageToken(t *testing.T) {
	for page, want := range map[string]string{
		"/s/info/JFF78dtmp2":  "JFF78dtmp2",
		"/s/info/3PuqEgKNz8a": "3PuqEgKNz8a",
		"/s/info/abc/":        "abc",
		"bare":                "bare",
		"":                    "",
	} {
		if got := pageToken(page); got != want {
			t.Errorf("pageToken(%q) = %q, want %q", page, got, want)
		}
	}
}
