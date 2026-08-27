// SPDX-License-Identifier: AGPL-3.0-or-later

package todoist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultEndpoint is the API v1 sync endpoint.
const DefaultEndpoint = "https://api.todoist.com/api/v1/sync"

// DefaultResourceTypes names every resource that the importer maps. The sync
// endpoint sends completed_info in the same answer, so the counts of the
// archived tasks cost no extra request.
var DefaultResourceTypes = []string{"projects", "items", "labels", "sections", "notes", "filters", "completed_info"}

// maxPages stops a cursor loop that never ends. A full sync of a personal
// account answers in one request, so this limit is only a guard.
const maxPages = 50

// maxBody limits what the client reads from one answer. The documented request
// limit of 1 MiB applies to the body that the client sends, and every request
// here is a few hundred bytes, but an answer has no documented ceiling.
const maxBody = 64 << 20

// Client reads a Todoist account. The client paces itself: it never sends more
// than one request per second, and it waits again after a 429 or a 5xx.
type Client struct {
	token    string
	Endpoint string
	HTTP     *http.Client

	// MinInterval is the smallest gap between two requests.
	MinInterval time.Duration
	// MaxAttempts counts the first try and every retry together.
	MaxAttempts int
	// MaxBackoff caps one wait, so a bad Retry-After header cannot park the
	// import for an hour.
	MaxBackoff time.Duration
	// Sleep waits. A test replaces it to run without real time.
	Sleep func(time.Duration)
	// Requests counts the HTTP requests that the client sent.
	Requests int
	// Waits records every wait, so a test can prove the backoff.
	Waits []time.Duration

	last time.Time
}

// New returns a client for one account token.
func New(token string) *Client {
	return &Client{
		token:       token,
		Endpoint:    DefaultEndpoint,
		HTTP:        &http.Client{Timeout: 60 * time.Second},
		MinInterval: time.Second,
		MaxAttempts: 5,
		MaxBackoff:  60 * time.Second,
		Sleep:       time.Sleep,
	}
}

// Sync reads the whole account. It sends one full sync, then follows a cursor
// while the server sends one.
func (c *Client) Sync(ctx context.Context, resources []string) (*Sync, error) {
	if len(resources) == 0 {
		resources = DefaultResourceTypes
	}
	types, err := json.Marshal(resources)
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("sync_token", "*")
	form.Set("resource_types", string(types))

	out, err := c.post(ctx, form)
	if err != nil {
		return nil, err
	}
	cursor := out.NextCursor
	for page := 0; cursor != "" && page < maxPages; page++ {
		form.Set("cursor", cursor)
		next, err := c.post(ctx, form)
		if err != nil {
			return nil, err
		}
		out.merge(next)
		if next.NextCursor == cursor { // the server repeats itself; stop
			break
		}
		cursor = next.NextCursor
	}
	return out, nil
}

func (c *Client) post(ctx context.Context, form url.Values) (*Sync, error) {
	var lastErr error
	attempts := c.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		c.pace()
		body, status, retryAfter, err := c.do(ctx, form)
		if err != nil {
			if ctx.Err() != nil {
				return nil, c.redact(err)
			}
			lastErr = c.redact(err)
		} else if status == http.StatusOK {
			var out Sync
			if err := json.Unmarshal(body, &out); err != nil {
				return nil, fmt.Errorf("the sync answer did not parse: %w", c.redact(err))
			}
			return &out, nil
		} else if status == http.StatusTooManyRequests || status >= 500 {
			lastErr = fmt.Errorf("todoist answered %d: %s", status, excerpt(c.redactText(string(body))))
		} else {
			// A 401 or a 400 does not improve with a retry.
			return nil, fmt.Errorf("todoist answered %d: %s", status, excerpt(c.redactText(string(body))))
		}
		if attempt == attempts-1 {
			break
		}
		c.wait(backoff(attempt, retryAfter, c.MaxBackoff))
	}
	return nil, lastErr
}

// do sends one request and returns the body, the status and the Retry-After
// value in seconds.
func (c *Client) do(ctx context.Context, form url.Values) ([]byte, int, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "teha-import/0.1")

	c.Requests++
	c.last = time.Now()
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, resp.StatusCode, 0, err
	}
	return body, resp.StatusCode, retryAfter(resp.Header.Get("Retry-After")), nil
}

// pace holds the rate under one request per second.
func (c *Client) pace() {
	if c.MinInterval <= 0 || c.last.IsZero() {
		return
	}
	if gap := c.MinInterval - time.Since(c.last); gap > 0 {
		c.wait(gap)
	}
}

func (c *Client) wait(d time.Duration) {
	if d <= 0 {
		return
	}
	c.Waits = append(c.Waits, d)
	sleep := c.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	sleep(d)
}

// redact keeps the token out of an error message. A transport error carries
// the request URL, and a future version of this client can put the token in a
// query, so the guard costs little.
func (c *Client) redact(err error) error {
	if err == nil {
		return nil
	}
	text := c.redactText(err.Error())
	if text == err.Error() {
		return err
	}
	return errors.New(text)
}

func (c *Client) redactText(text string) string {
	if c.token == "" {
		return text
	}
	return strings.ReplaceAll(text, c.token, "[redacted]")
}

// backoff returns the wait before the next try: the Retry-After value when the
// server sends one, else one, two, four seconds and so on.
func backoff(attempt int, header, max time.Duration) time.Duration {
	d := header
	if d <= 0 {
		d = time.Second << attempt
	}
	if max > 0 && d > max {
		d = max
	}
	return d
}

// retryAfter reads the header in both documented forms: a number of seconds,
// or an HTTP date.
func retryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if n, err := strconv.Atoi(value); err == nil {
		if n < 0 {
			return 0
		}
		return time.Duration(n) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}

func excerpt(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > 200 {
		return text[:200] + "..."
	}
	if text == "" {
		return "(no body)"
	}
	return text
}
