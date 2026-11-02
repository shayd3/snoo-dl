// Package reddit is a small client for Reddit's public listing endpoints.
package reddit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is used for unauthenticated requests.
	DefaultBaseURL = "https://www.reddit.com"
	// DefaultOAuthBaseURL is used once an OAuth token has been obtained.
	DefaultOAuthBaseURL = "https://oauth.reddit.com"
	// DefaultTokenURL is Reddit's OAuth token endpoint.
	DefaultTokenURL = "https://www.reddit.com/api/v1/access_token"

	// MaxPageSize is the largest page Reddit returns for a listing.
	MaxPageSize = 100

	maxResponseBytes = 10 << 20 // 10 MiB is far more than any listing page
	maxBackoff       = 60 * time.Second
)

var (
	// Sorts are the listing sorts snoo-dl supports.
	Sorts = []string{"hot", "new", "top", "rising", "controversial"}
	// Times are the time windows accepted by the top and controversial sorts.
	Times = []string{"hour", "day", "week", "month", "year", "all"}

	subredditPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_]{1,20}$`)

	// ErrNotFound is returned when a subreddit does not exist.
	ErrNotFound = errors.New("subreddit not found")
	// ErrForbidden is returned when a subreddit is private, quarantined or banned.
	ErrForbidden = errors.New("subreddit is private, quarantined or banned")
)

// IsValidSort reports whether s is a supported listing sort.
func IsValidSort(s string) bool { return contains(Sorts, strings.ToLower(s)) }

// IsValidTime reports whether t is a supported time window.
func IsValidTime(t string) bool { return contains(Times, strings.ToLower(t)) }

// SortUsesTime reports whether the sort accepts a time window.
func SortUsesTime(sort string) bool { return sort == "top" || sort == "controversial" }

func contains(values []string, v string) bool {
	for _, value := range values {
		if value == v {
			return true
		}
	}
	return false
}

// NormalizeSubreddit strips an optional "r/" or "/r/" prefix and validates the
// name. Multireddits written as "a+b" are accepted and each part is validated.
func NormalizeSubreddit(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	name = strings.TrimPrefix(name, "/")
	if len(name) > 2 && strings.EqualFold(name[:2], "r/") {
		name = name[2:]
	}
	name = strings.TrimSuffix(name, "/")
	if name == "" {
		return "", fmt.Errorf("invalid subreddit %q", raw)
	}
	for _, part := range strings.Split(name, "+") {
		if !subredditPattern.MatchString(part) {
			return "", fmt.Errorf("invalid subreddit %q: names are 2-21 letters, digits or underscores", raw)
		}
	}
	return name, nil
}

// ListingOptions selects a listing page.
type ListingOptions struct {
	Sort  string // one of Sorts; defaults to "top"
	Time  string // one of Times; only sent for sorts where SortUsesTime
	After string // pagination cursor from a previous page
	Limit int    // page size, capped at MaxPageSize
}

// Client fetches listings from Reddit. The zero value is not usable; set at
// least HTTP and UserAgent, or use New.
type Client struct {
	HTTP      *http.Client
	UserAgent string

	// BaseURL is the host used for unauthenticated requests.
	BaseURL string

	// MaxRetries is how many times a request is retried after a 429, a 5xx or
	// a network error.
	MaxRetries int

	// ClientID and ClientSecret enable app-only OAuth when both are set.
	ClientID     string
	ClientSecret string
	TokenURL     string
	OAuthBaseURL string

	// Sleep waits for d or until ctx is done. Tests replace it.
	Sleep func(ctx context.Context, d time.Duration) error

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
	nextAllowed time.Time
}

// New returns a client with Reddit's default endpoints.
func New(httpClient *http.Client, userAgent string) *Client {
	return &Client{
		HTTP:         httpClient,
		UserAgent:    userAgent,
		BaseURL:      DefaultBaseURL,
		MaxRetries:   3,
		TokenURL:     DefaultTokenURL,
		OAuthBaseURL: DefaultOAuthBaseURL,
	}
}

// Listing fetches one page of posts from subreddit.
func (c *Client) Listing(ctx context.Context, subreddit string, opts ListingOptions) (Listing, error) {
	var listing Listing

	sort := strings.ToLower(opts.Sort)
	if sort == "" {
		sort = "top"
	}
	if !IsValidSort(sort) {
		return listing, fmt.Errorf("invalid sort %q", opts.Sort)
	}

	limit := opts.Limit
	if limit <= 0 || limit > MaxPageSize {
		limit = MaxPageSize
	}

	query := url.Values{}
	query.Set("limit", strconv.Itoa(limit))
	query.Set("raw_json", "1")
	if SortUsesTime(sort) && opts.Time != "" {
		query.Set("t", strings.ToLower(opts.Time))
	}
	if opts.After != "" {
		query.Set("after", opts.After)
	}

	base, authHeader, err := c.endpoint(ctx)
	if err != nil {
		return listing, err
	}
	requestURL := fmt.Sprintf("%s/r/%s/%s.json?%s", strings.TrimSuffix(base, "/"), subreddit, sort, query.Encode())

	resp, err := c.do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, err
		}
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		return req, nil
	})
	if err != nil {
		return listing, err
	}
	defer resp.Body.Close()

	// Reddit redirects unknown subreddits to a search page.
	if resp.Request != nil && strings.Contains(resp.Request.URL.Path, "/subreddits/search") {
		return listing, fmt.Errorf("r/%s: %w", subreddit, ErrNotFound)
	}

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return listing, fmt.Errorf("r/%s: %w", subreddit, ErrNotFound)
	case http.StatusForbidden:
		return listing, fmt.Errorf("r/%s: %w", subreddit, ErrForbidden)
	default:
		return listing, fmt.Errorf("reddit request failed with status %s", resp.Status)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&listing); err != nil {
		return listing, fmt.Errorf("decoding reddit response: %w", err)
	}
	return listing, nil
}

// endpoint returns the base URL and Authorization header to use, fetching an
// OAuth token first if credentials are configured.
func (c *Client) endpoint(ctx context.Context) (string, string, error) {
	if c.ClientID == "" || c.ClientSecret == "" {
		return c.BaseURL, "", nil
	}
	token, err := c.accessToken(ctx)
	if err != nil {
		return "", "", err
	}
	base := c.OAuthBaseURL
	if base == "" {
		base = DefaultOAuthBaseURL
	}
	return base, "Bearer " + token, nil
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	tokenURL := c.TokenURL
	if tokenURL == "" {
		tokenURL = DefaultTokenURL
	}
	form := url.Values{"grant_type": {"client_credentials"}}.Encode()

	resp, err := c.do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form))
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth(c.ClientID, c.ClientSecret)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req, nil
	})
	if err != nil {
		return "", fmt.Errorf("reddit oauth: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("reddit oauth: token request failed with status %s", resp.Status)
	}

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("reddit oauth: decoding token: %w", err)
	}
	if body.AccessToken == "" {
		return "", fmt.Errorf("reddit oauth: no access token returned (%s)", body.Error)
	}

	expiresIn := time.Duration(body.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = time.Hour
	}
	c.mu.Lock()
	c.token = body.AccessToken
	c.tokenExpiry = time.Now().Add(expiresIn - time.Minute)
	c.mu.Unlock()
	return body.AccessToken, nil
}

// do sends a request, waiting out Reddit's rate limit and retrying on 429,
// 5xx and network errors. newReq is called once per attempt.
func (c *Client) do(ctx context.Context, newReq func() (*http.Request, error)) (*http.Response, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		if err := c.waitForRateLimit(ctx); err != nil {
			return nil, err
		}

		req, err := newReq()
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.UserAgent)

		resp, err := c.HTTP.Do(req)
		var wait time.Duration
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			wait = backoff(attempt)
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("reddit request failed with status %s", resp.Status)
			wait = retryAfter(resp.Header, backoff(attempt))
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
		default:
			c.recordRateLimit(resp.Header)
			return resp, nil
		}

		if attempt >= c.MaxRetries {
			return nil, lastErr
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// recordRateLimit notes when the next request may be sent, based on Reddit's
// X-Ratelimit-Remaining and X-Ratelimit-Reset headers.
func (c *Client) recordRateLimit(h http.Header) {
	remaining, err := strconv.ParseFloat(h.Get("X-Ratelimit-Remaining"), 64)
	if err != nil || remaining >= 1 {
		return
	}
	reset, err := strconv.ParseFloat(h.Get("X-Ratelimit-Reset"), 64)
	if err != nil || reset <= 0 {
		return
	}
	wait := time.Duration(reset * float64(time.Second))
	if wait > maxBackoff*10 {
		wait = maxBackoff * 10
	}
	c.mu.Lock()
	c.nextAllowed = time.Now().Add(wait)
	c.mu.Unlock()
}

func (c *Client) waitForRateLimit(ctx context.Context) error {
	c.mu.Lock()
	wait := time.Until(c.nextAllowed)
	c.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	return c.sleep(ctx, wait)
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func backoff(attempt int) time.Duration {
	d := time.Second << attempt
	if d <= 0 || d > maxBackoff {
		return maxBackoff
	}
	return d
}

func retryAfter(h http.Header, fallback time.Duration) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return min(time.Duration(secs)*time.Second, maxBackoff)
		}
		if t, err := http.ParseTime(v); err == nil {
			return min(max(time.Until(t), 0), maxBackoff)
		}
	}
	if reset, err := strconv.ParseFloat(h.Get("X-Ratelimit-Reset"), 64); err == nil && reset > 0 {
		return min(time.Duration(reset*float64(time.Second)), maxBackoff)
	}
	return fallback
}
