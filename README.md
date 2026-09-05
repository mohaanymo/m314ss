# m314-sub

Finds a subtitle for a video, checks that it actually belongs to that video,
and fixes the timing. Go library and CLI. Only external dependency is `ffmpeg`
on PATH.

```
go get github.com/mohaanymo/m314-sub
```

The package is named `subtitles`, so import it with an alias:

```go
import subtitles "github.com/mohaanymo/m314-sub"
```

## The problem

Subtitle sites have dozens of files per title, cut for different releases,
and their episode tagging is not trustworthy. The most downloaded "S01E01"
file for one show I tried was actually episode 2. Nothing in the metadata
tells you that.

The audio does. If a subtitle belongs to a video, its cue start times line up
with the moments speech begins. If it doesn't, they don't. So every candidate
gets checked against the video's audio before it's accepted, and the offset
correction falls out of the same measurement.

## CLI

```
go build ./cmd/subtitles
export SUBDL_API_KEY=...
export OPENSUBTITLES_API_KEY=... OPENSUBTITLES_USER=... OPENSUBTITLES_PASS=...   # optional

subtitles -tmdb 1396 -s 1 -e 1 -lang ar -video Breaking.Bad.S01E01.mkv
```

Writes `Breaking.Bad.S01E01.ar.srt`, already shifted. Exit code 1 if nothing
matched. Other useful flags:

| flag | |
|---|---|
| `-imdb tt0903747` | search by IMDb id instead of TMDB |
| `-title "..." -year 2021` | free text search when you have no id |
| `-o path` | output path |
| `-tries N` | candidates to download per source before giving up (default 3) |
| `-ffmpeg path` | ffmpeg binary, if not on PATH |
| `-serve :8081` | run as an HTTP service, see below |

Skipping `-video` still works but then nothing is verified and you get
whatever downloads first.

## Library

```go
speech, err := subtitles.Listen("/media/Breaking.Bad.S01E01.mkv")

c := subtitles.Client{
    Sources: []subtitles.Source{
        &subtitles.SubDL{APIKey: subdlKey},
        &subtitles.OpenSubtitles{APIKey: osKey, Username: u, Password: p},
    },
    Speech: speech, // nil skips verification
    Log:    func(s string) { log.Println(s) },
}

res, err := c.Fetch(ctx, subtitles.Query{TMDBID: 1396, Season: 1, Episode: 1, Lang: "ar"})
if errors.Is(err, subtitles.ErrNoMatch) {
    // nothing on offer was for this video. ship without a subtitle
    // rather than attach a wrong one.
}
os.WriteFile(dst, res.Data, 0o644)
```

`Listen` is the expensive part (a few seconds of ffmpeg). Matching is
milliseconds, so keep the `Speech` around and test as many candidates as you
want against it:

```go
for _, data := range candidates {
    res, fixed, _ := speech.Match(data)
    if res.Matched() { ... }
}
```

Already have a subtitle and only want it retimed:

```go
res, fixed, err := subtitles.Sync(videoPath, srtBytes)
```

Adding a source means implementing `Find(ctx, Query) ([]Candidate, error)`.
Downloading is deferred behind `Candidate.Fetch` so a metered source only
pays for the candidates that actually get tried.

## HTTP service

```
subtitles -serve :8081
```

`POST /subtitle` takes the same fields as `Query` plus `onsets` and `windows`
from the caller's own `Listen`. The caller does the listening and sends a few
hundred floats, the service does the searching. That keeps site credentials
off every user's machine while the party holding the video still decides what
counts as a match.

```
POST /subtitle    {"tmdb_id":1396,"season":1,"episode":1,"lang":"ar","onsets":[...],"windows":[[...]]}
GET  /health
```

The reply carries the SRT, source, release name, offset, hit rate, baseline and
sigma. 404 when nothing matched. There is no auth, put it behind whatever
already authenticates your callers.

## Sources

**SubDL** goes first. Downloads aren't metered on the free tier (2000
searches/day) and one download usually returns a whole season pack, which the
episode picker then extracts from. Its catalogue has holes on niche and older
titles.

**OpenSubtitles** covers those holes. Downloads are capped (20/day free, 1000
on VIP) so it goes second and only gets hit once subdl has come back empty.

Either one alone is enough to run.

## How the matching works

Speech onsets are pulled from a sample of the audio (ten 180 second windows
spread over the runtime, or the whole thing if it's short) using a simple
energy threshold VAD. Then the subtitle's cue starts are slid against them in
50ms steps over a +/-120s range to find the offset that explains the most cues.

Two things it deliberately does not do:

- Compare speech/silence masks. With a continuous score the speech mask is
  ~80% true, every offset overlaps about equally and the winner is noise. I
  got +4.6s for a subtitle whose real offset was -110s. Onsets stay sparse no
  matter how loud the film is.
- Gate on raw hit rate. A correct subtitle scores ~58% on a clean live action
  film but only ~42% on a low bitrate cartoon, which is below what a *wrong*
  subtitle scores on the easier film. The gate is sigma (how far the best
  offset stands out from every other offset tried) plus a minimum lift over
  chance. See `MinSigma` and `MinLift` in `align.go` for the numbers.

Only constant offsets are corrected. A subtitle timed for a different
framerate drifts linearly and will be rejected rather than half fixed.

## SubDL pro / AI

`ai.go` implements subdl's v2 endpoints: translate a subtitle into a language
nobody uploaded, transcribe a media URL, resolve a release filename to a
title. With `Client.TranslateWith` set, a title with no Arabic subtitle gets
searched in other languages, verified against the audio the same way, and the
one that fits gets translated.

As of writing the v2 API is not deployed (every path 404s, with or without a
key) so this code follows their docs but hasn't been run against a live
endpoint. The translation fallback degrades to `ErrNoMatch` until then.

## License

MIT
