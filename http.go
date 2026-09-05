package subtitles

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxBody = 32 << 20 // subtitle archives are small

func defaultClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func do(ctx context.Context, c *http.Client, req *http.Request, h http.Header) ([]byte, error) {
	for k, vs := range h {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	resp, err := defaultClient(c).Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s: %s", req.URL.Host, resp.Status, snippet(data))
	}
	return data, nil
}

func getBytes(ctx context.Context, c *http.Client, url string, h http.Header) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return do(ctx, c, req, h)
}

func getJSON(ctx context.Context, c *http.Client, url string, h http.Header, out any) error {
	data, err := getBytes(ctx, c, url, h)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func postBytes(ctx context.Context, c *http.Client, url string, h http.Header, in any) ([]byte, error) {
	payload, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	if h == nil {
		h = http.Header{}
	}
	if h.Get("Content-Type") == "" {
		h.Set("Content-Type", "application/json")
	}
	return do(ctx, c, req, h)
}

func snippet(b []byte) string {
	if len(b) > 200 {
		b = b[:200]
	}
	return string(bytes.TrimSpace(b))
}
