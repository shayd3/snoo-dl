package reddit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func noSleep(_ context.Context, _ time.Duration) error { return nil }

func TestNormalizeSubreddit(t *testing.T) {
	t.Parallel()
	valid := map[string]string{
		"wallpapers":      "wallpapers",
		"r/wallpapers":    "wallpapers",
		"/r/EarthPorn/":   "EarthPorn",
		"R/pics":          "pics",
		"wallpapers+pics": "wallpapers+pics",
		" spaced_name_1 ": "spaced_name_1",
	}
	for in, want := range valid {
		got, err := NormalizeSubreddit(in)
		if err != nil || got != want {
			t.Errorf("NormalizeSubreddit(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	for _, in := range []string{"", "a", "../../api/foo?x=", "pics/top", "has space", "toolongsubredditname_123", "a++b", "_leading"} {
		if _, err := NormalizeSubreddit(in); err == nil {
			t.Errorf("NormalizeSubreddit(%q) should fail", in)
		}
	}
}

func TestValidSortsAndTimes(t *testing.T) {
	t.Parallel()
	if !IsValidSort("TOP") || IsValidSort("best") {
		t.Fatal("sort validation is wrong")
	}
	if !IsValidTime("Week") || IsValidTime("weekday") {
		t.Fatal("time validation is wrong")
	}
}

func listingJSON(t *testing.T, w http.ResponseWriter, after string, ids ...string) {
	t.Helper()
	var out Listing
	for _, id := range ids {
		out.Data.Children = append(out.Data.Children, Post{Data: PostData{ID: id}})
	}
	out.Data.After = after
	if err := json.NewEncoder(w).Encode(out); err != nil {
		t.Errorf("encode: %v", err)
	}
}

func TestListingBuildsRequest(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/r/pics/top.json" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("t") != "month" || q.Get("limit") != "25" || q.Get("after") != "t3_x" || q.Get("raw_json") != "1" {
			t.Errorf("unexpected query %q", r.URL.RawQuery)
		}
		if ua := r.Header.Get("User-Agent"); ua != "test-agent" {
			t.Errorf("unexpected user agent %q", ua)
		}
		listingJSON(t, w, "next", "a", "b")
	}))
	defer server.Close()

	c := New(server.Client(), "test-agent")
	c.BaseURL = server.URL
	got, err := c.Listing(context.Background(), "pics", ListingOptions{Sort: "top", Time: "month", After: "t3_x", Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Data.Children) != 2 || got.Data.After != "next" {
		t.Fatalf("unexpected listing %+v", got)
	}
}

func TestListingOmitsTimeForHot(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/r/pics/hot.json" || r.URL.Query().Has("t") {
			t.Errorf("unexpected request %s", r.URL)
		}
		listingJSON(t, w, "")
	}))
	defer server.Close()

	c := New(server.Client(), "ua")
	c.BaseURL = server.URL
	if _, err := c.Listing(context.Background(), "pics", ListingOptions{Sort: "hot", Time: "week"}); err != nil {
		t.Fatal(err)
	}
}

func TestListingRetriesOn429(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		listingJSON(t, w, "", "a")
	}))
	defer server.Close()

	var waits []time.Duration
	c := New(server.Client(), "ua")
	c.BaseURL = server.URL
	c.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }

	got, err := c.Listing(context.Background(), "pics", ListingOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Data.Children) != 1 || calls.Load() != 3 {
		t.Fatalf("calls=%d listing=%+v", calls.Load(), got)
	}
	if len(waits) != 2 || waits[0] != 7*time.Second {
		t.Fatalf("expected two 7s waits from Retry-After, got %v", waits)
	}
}

func TestListingGivesUpAfterMaxRetries(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	c := New(server.Client(), "ua")
	c.BaseURL = server.URL
	c.MaxRetries = 2
	c.Sleep = noSleep
	if _, err := c.Listing(context.Background(), "pics", ListingOptions{}); err == nil {
		t.Fatal("expected an error")
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls.Load())
	}
}

func TestListingErrors(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/r/private/top.json", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	mux.HandleFunc("/r/missing/top.json", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/r/redirected/top.json", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/subreddits/search.json?q=redirected", http.StatusFound)
	})
	mux.HandleFunc("/subreddits/search.json", func(w http.ResponseWriter, _ *http.Request) { listingJSON(t, w, "") })
	mux.HandleFunc("/r/garbage/top.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) })
	server := httptest.NewServer(mux)
	defer server.Close()

	c := New(server.Client(), "ua")
	c.BaseURL = server.URL

	if _, err := c.Listing(context.Background(), "private", ListingOptions{}); !errors.Is(err, ErrForbidden) {
		t.Errorf("private: got %v", err)
	}
	if _, err := c.Listing(context.Background(), "missing", ListingOptions{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: got %v", err)
	}
	if _, err := c.Listing(context.Background(), "redirected", ListingOptions{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("redirected: got %v", err)
	}
	if _, err := c.Listing(context.Background(), "garbage", ListingOptions{}); err == nil {
		t.Error("garbage: expected decode error")
	}
	if _, err := c.Listing(context.Background(), "x", ListingOptions{Sort: "best"}); err == nil {
		t.Error("invalid sort: expected error")
	}
}

func TestRateLimitHeadersDelayNextRequest(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Ratelimit-Remaining", "0")
		w.Header().Set("X-Ratelimit-Reset", "42")
		listingJSON(t, w, "")
	}))
	defer server.Close()

	var waits []time.Duration
	c := New(server.Client(), "ua")
	c.BaseURL = server.URL
	c.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }

	for range 2 {
		if _, err := c.Listing(context.Background(), "pics", ListingOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(waits) != 1 || waits[0] < 40*time.Second || waits[0] > 42*time.Second {
		t.Fatalf("expected one ~42s wait before the second request, got %v", waits)
	}
}

func TestOAuth(t *testing.T) {
	t.Parallel()
	var tokenCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/access_token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		user, pass, ok := r.BasicAuth()
		if !ok || user != "id" || pass != "secret" || r.FormValue("grant_type") != "client_credentials" {
			t.Errorf("bad token request: %v %q %q %q", ok, user, pass, r.FormValue("grant_type"))
		}
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
	})
	mux.HandleFunc("/r/pics/top.json", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("unexpected Authorization %q", got)
		}
		listingJSON(t, w, "")
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := New(server.Client(), "ua")
	c.BaseURL = "http://unused.invalid"
	c.OAuthBaseURL = server.URL
	c.TokenURL = server.URL + "/api/v1/access_token"
	c.ClientID, c.ClientSecret = "id", "secret"

	for range 2 {
		if _, err := c.Listing(context.Background(), "pics", ListingOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if tokenCalls.Load() != 1 {
		t.Fatalf("expected the token to be cached, got %d token calls", tokenCalls.Load())
	}
}

func TestListingHonorsCancellation(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := New(server.Client(), "ua")
	c.BaseURL = server.URL
	c.Sleep = func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
	if _, err := c.Listing(ctx, "pics", ListingOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
