package downloader

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shayd3/snoo-dl/internal/reddit"
)

func TestFilterMatches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		filter Filter
		w, h   int
		want   bool
	}{
		{"empty filter passes unknown size", Filter{}, 0, 0, true},
		{"unknown size fails a filter", Filter{MinWidth: 1}, 0, 0, false},
		{"exact resolution", Filter{Width: 1920, Height: 1080}, 1920, 1080, true},
		{"wrong resolution", Filter{Width: 1920, Height: 1080}, 1080, 1080, false},
		{"exact ratio", Filter{AspectWidth: 16, AspectHeight: 9}, 3840, 2160, true},
		{"1366x768 is not exactly 16:9", Filter{AspectWidth: 16, AspectHeight: 9}, 1366, 768, false},
		{"1366x768 within 1%", Filter{AspectWidth: 16, AspectHeight: 9, AspectTolerance: 0.01}, 1366, 768, true},
		{"4:3 not within 1% of 16:9", Filter{AspectWidth: 16, AspectHeight: 9, AspectTolerance: 0.01}, 1600, 1200, false},
		{"min width ok", Filter{MinWidth: 2560}, 2560, 1440, true},
		{"min width too small", Filter{MinWidth: 2560}, 1920, 1080, false},
		{"min height too small", Filter{MinHeight: 1440}, 2560, 1080, false},
	}
	for _, tt := range tests {
		if got := tt.filter.Matches(tt.w, tt.h); got != tt.want {
			t.Errorf("%s: Matches(%d, %d) = %v, want %v", tt.name, tt.w, tt.h, got, tt.want)
		}
	}
}

func TestParsePair(t *testing.T) {
	t.Parallel()
	if w, h, err := ParsePair("1920 x 1080", "x", "resolution"); err != nil || w != 1920 || h != 1080 {
		t.Fatalf("got %d %d %v", w, h, err)
	}
	for _, bad := range []string{"1920", "16x9x1", "0x10", "ax10", "10x-1"} {
		if _, _, err := ParsePair(bad, "x", "resolution"); err == nil {
			t.Errorf("ParsePair(%q) should fail", bad)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"Hello /r/wallpapers: 4K?": "Hello__r_wallpapers__4K",
		"":                         "reddit_image",
		"../../etc/passwd":         "etc_passwd",
		"...":                      "reddit_image",
		"CON":                      "_CON",
		"nul.txt":                  "_nul.txt",
		"\x1b[31mred":              "31mred",
	}
	for in, want := range tests {
		if got := SanitizeFilename(in); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}

	long := strings.Repeat("é", 200) // 400 bytes
	got := SanitizeFilename(long)
	if len(got) > maxTitleBytes || !strings.HasPrefix(long, got) {
		t.Fatalf("expected truncation on a rune boundary, got %d bytes", len(got))
	}
}

func TestFileName(t *testing.T) {
	t.Parallel()
	if got := FileName("Untitled", "abc123", 0, ".jpg"); got != "Untitled_abc123.jpg" {
		t.Errorf("got %q", got)
	}
	if got := FileName("Untitled", "abc123", 3, ".png"); got != "Untitled_abc123_3.png" {
		t.Errorf("got %q", got)
	}
	if got := FileName("Untitled", "", 0, ".png"); got != "Untitled.png" {
		t.Errorf("got %q", got)
	}
}

func TestPrintable(t *testing.T) {
	t.Parallel()
	if got := Printable("a\x1b]0;evil\x07b\nc"); got != "a]0;evilbc" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractCandidatesSinglePost(t *testing.T) {
	t.Parallel()
	post := reddit.PostData{
		URL:                 "https://i.redd.it/from-url.jpg",
		URLOverriddenByDest: "https://i.redd.it/from-overridden.png",
		Preview: reddit.Preview{Images: []reddit.PreviewImage{{
			Source: reddit.ImageSource{URL: "https://preview.redd.it/x.jpg?a=1&amp;b=2", Width: 1920, Height: 1080},
		}}},
	}
	got := ExtractCandidates(post)
	if len(got) != 1 || got[0].URL != "https://i.redd.it/from-overridden.png" || got[0].Width != 1920 || got[0].Index != 0 {
		t.Fatalf("unexpected candidates %+v", got)
	}
}

func TestExtractCandidatesGallery(t *testing.T) {
	t.Parallel()
	post := reddit.PostData{
		IsGallery: true,
		URL:       "https://www.reddit.com/gallery/abc",
		GalleryData: reddit.GalleryData{Items: []reddit.GalleryItem{
			{MediaID: "one"}, {MediaID: "failed"}, {MediaID: "anim"}, {MediaID: "missing"}, {MediaID: "four"},
		}},
		MediaMetadata: map[string]reddit.MediaMeta{
			"one":    {Status: "valid", E: "Image", S: reddit.MediaSource{U: "https://preview.redd.it/one.jpg?width=3840&amp;format=pjpg&amp;s=sig", X: 3840, Y: 2160}},
			"failed": {Status: "failed", S: reddit.MediaSource{U: "https://preview.redd.it/failed.jpg"}},
			"anim":   {Status: "valid", E: "AnimatedImage", S: reddit.MediaSource{Gif: "https://i.redd.it/anim.gif", MP4: "https://preview.redd.it/anim.gif?format=mp4", X: 500, Y: 500}},
			"four":   {Status: "valid", E: "Image", S: reddit.MediaSource{U: "https://preview.redd.it/four.png?width=100", X: 100, Y: 100}},
		},
	}
	got := ExtractCandidates(post)
	want := []Candidate{
		{URL: "https://i.redd.it/one.jpg", Width: 3840, Height: 2160, Index: 1},
		{URL: "https://i.redd.it/anim.gif", Width: 500, Height: 500, Index: 3},
		{URL: "https://i.redd.it/four.png", Width: 100, Height: 100, Index: 5},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestExtractCandidatesCrosspostAndImgur(t *testing.T) {
	t.Parallel()
	post := reddit.PostData{
		URL: "https://www.reddit.com/r/pics/comments/abc/",
		CrosspostParentList: []reddit.PostData{{
			URL: "https://imgur.com/AbC123x",
		}},
	}
	got := ExtractCandidates(post)
	if len(got) != 1 || got[0].URL != "https://i.imgur.com/AbC123x.jpg" {
		t.Fatalf("unexpected candidates %+v", got)
	}

	cases := map[string]string{
		"https://i.imgur.com/abcde.gifv":    "https://i.imgur.com/abcde.gif",
		"https://i.imgur.com/abcde.png":     "https://i.imgur.com/abcde.png",
		"https://imgur.com/a/album123":      "",
		"https://imgur.com/gallery/abcde":   "",
		"https://v.redd.it/xyz":             "",
		"https://example.com/no-ext":        "",
		"https://example.com/x?format=webp": "https://example.com/x?format=webp",
		"not a url":                         "",
	}
	for in, want := range cases {
		got, ok := normalizeImageURL(in)
		if want == "" {
			if ok {
				t.Errorf("normalizeImageURL(%q) = %q, want unsupported", in, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("normalizeImageURL(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestValidateURL(t *testing.T) {
	t.Parallel()
	for raw, ok := range map[string]bool{
		"https://i.redd.it/a.jpg":           true,
		"http://i.redd.it/a.jpg":            false,
		"file:///etc/passwd":                false,
		"https:///nohost.jpg":               false,
		"http://169.254.169.254/latest.png": false,
	} {
		u, _ := url.Parse(raw)
		if err := ValidateURL(u); (err == nil) != ok {
			t.Errorf("ValidateURL(%q) err=%v, want ok=%v", raw, err, ok)
		}
	}
}

func TestIsPublic(t *testing.T) {
	t.Parallel()
	for addr, want := range map[string]bool{
		"151.101.1.140":    true,
		"2a04:4e42::396":   true,
		"127.0.0.1":        false,
		"10.1.2.3":         false,
		"192.168.1.1":      false,
		"169.254.169.254":  false,
		"100.64.0.1":       false,
		"::1":              false,
		"fe80::1":          false,
		"fd00::1":          false,
		"0.0.0.0":          false,
		"::ffff:127.0.0.1": false,
	} {
		if got := isPublic(netip.MustParseAddr(addr)); got != want {
			t.Errorf("isPublic(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestBlockNonPublicDial(t *testing.T) {
	t.Parallel()
	if err := blockNonPublic("tcp", "127.0.0.1:443", nil); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("expected ErrBlockedAddress, got %v", err)
	}
	if err := blockNonPublic("tcp", "151.101.1.140:443", nil); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestCheckRedirect(t *testing.T) {
	t.Parallel()
	req := func(raw string) *http.Request {
		r, _ := http.NewRequest(http.MethodGet, raw, nil)
		return r
	}
	if err := checkRedirect(req("http://localhost/a.jpg"), nil); err == nil {
		t.Error("expected redirect to http to be refused")
	}
	via := make([]*http.Request, maxRedirects)
	if err := checkRedirect(req("https://i.redd.it/a.jpg"), via); err == nil {
		t.Error("expected too many redirects to be refused")
	}
	if err := checkRedirect(req("https://i.redd.it/a.jpg"), via[:1]); err != nil {
		t.Errorf("unexpected error %v", err)
	}
}

type testEnv struct {
	server *httptest.Server
	dir    string
	out    *bytes.Buffer
	errOut *bytes.Buffer
}

func newTestEnv(t *testing.T, handler http.Handler) *testEnv {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return &testEnv{server: server, dir: t.TempDir(), out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
}

func (e *testEnv) downloader(opts Options) *Downloader {
	opts.Location = e.dir
	return &Downloader{HTTP: e.server.Client(), UserAgent: "test", Options: opts, Out: e.out, Err: e.errOut}
}

func imagePost(id, title, u string) reddit.PostData {
	return reddit.PostData{ID: id, Title: title, URL: u}
}

func run(t *testing.T, d *Downloader, posts ...reddit.PostData) Stats {
	t.Helper()
	ctx := context.Background()
	d.Start(ctx)
	for _, p := range posts {
		if err := d.Submit(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	return d.Wait()
}

func TestDownloadSavesAndSkipsExisting(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "test" {
			t.Errorf("missing user agent")
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-bytes"))
	}))

	posts := []reddit.PostData{
		imagePost("a1", "Untitled", env.server.URL+"/a.jpg"),
		imagePost("b2", "Untitled", env.server.URL+"/b.jpg"), // same title, different post
	}
	stats := run(t, env.downloader(Options{Concurrency: 2}), posts...)
	if stats.Downloaded != 2 || stats.Failed != 0 || stats.Bytes != 20 {
		t.Fatalf("unexpected stats %+v (stderr: %s)", stats, env.errOut)
	}
	for _, name := range []string{"Untitled_a1.jpg", "Untitled_b2.jpg"} {
		data, err := os.ReadFile(filepath.Join(env.dir, name))
		if err != nil || string(data) != "jpeg-bytes" {
			t.Fatalf("%s: %q %v", name, data, err)
		}
	}

	stats = run(t, env.downloader(Options{}), posts...)
	if stats.Existing != 2 || stats.Downloaded != 0 {
		t.Fatalf("expected both files to be skipped, got %+v", stats)
	}
}

func TestDownloadRejectsBadResponses(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/html.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not found</html>"))
	})
	mux.HandleFunc("/missing.jpg", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/big.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(bytes.Repeat([]byte("x"), 2048))
	})
	mux.HandleFunc("/big-declared.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "4096")
		_, _ = w.Write(bytes.Repeat([]byte("x"), 4096))
	})
	env := newTestEnv(t, mux)

	stats := run(t, env.downloader(Options{MaxBytes: 1024}),
		imagePost("h", "html", env.server.URL+"/html.jpg"),
		imagePost("m", "missing", env.server.URL+"/missing.jpg"),
		imagePost("b", "big", env.server.URL+"/big.jpg"),
		imagePost("d", "declared", env.server.URL+"/big-declared.jpg"),
	)
	if stats.Failed != 4 || stats.Downloaded != 0 {
		t.Fatalf("unexpected stats %+v", stats)
	}
	entries, _ := os.ReadDir(env.dir)
	if len(entries) != 0 {
		t.Fatalf("expected no files (including .part leftovers), found %v", entries)
	}
	if !strings.Contains(env.errOut.String(), "content type") || !strings.Contains(env.errOut.String(), "size limit") {
		t.Fatalf("unexpected error output: %s", env.errOut)
	}
}

func TestDownloadRefusesPlainHTTP(t *testing.T) {
	t.Parallel()
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("plain http server should not be contacted")
	}))
	defer plain.Close()
	env := newTestEnv(t, http.NotFoundHandler())

	stats := run(t, env.downloader(Options{}), imagePost("p", "plain", plain.URL+"/a.jpg"))
	if stats.Failed != 1 {
		t.Fatalf("unexpected stats %+v", stats)
	}
}

func TestDownloadCleansUpOnCancel(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	env := newTestEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))

	ctx, cancel := context.WithCancel(context.Background())
	d := env.downloader(Options{})
	d.Start(ctx)
	go func() { <-started; cancel() }()
	if err := d.Submit(ctx, imagePost("c", "cancel", env.server.URL+"/c.png")); err != nil {
		t.Fatal(err)
	}
	stats := d.Wait()
	if stats.Downloaded != 0 || stats.Failed != 1 {
		t.Fatalf("unexpected stats %+v", stats)
	}
	entries, _ := os.ReadDir(env.dir)
	if len(entries) != 0 {
		t.Fatalf("expected the partial file to be removed, found %v", entries)
	}
}

func TestPostFiltersAndDryRun(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("dry run should not download")
	}))

	nsfw := imagePost("n", "nsfw", env.server.URL+"/n.jpg")
	nsfw.Over18 = true
	lowScore := imagePost("l", "low", env.server.URL+"/l.jpg")
	lowScore.Score = 1
	good := imagePost("g", "good", env.server.URL+"/g.jpg")
	good.Score = 500
	small := imagePost("s", "small", env.server.URL+"/s.jpg")
	small.Score = 500
	small.Preview.Images = []reddit.PreviewImage{{Source: reddit.ImageSource{Width: 100, Height: 100}}}
	good.Preview.Images = []reddit.PreviewImage{{Source: reddit.ImageSource{Width: 4000, Height: 3000}}}

	stats := run(t, env.downloader(Options{DryRun: true, SkipNSFW: true, MinScore: 10, Filter: Filter{MinWidth: 1000}}),
		nsfw, lowScore, good, small)
	if stats.Planned != 1 || !strings.Contains(env.out.String(), "good_g.jpg") {
		t.Fatalf("unexpected result %+v: %s", stats, env.out)
	}

	env.out.Reset()
	stats = run(t, env.downloader(Options{DryRun: true, NSFWOnly: true}), nsfw, good)
	if stats.Planned != 1 || !strings.Contains(env.out.String(), "nsfw_n.jpg") {
		t.Fatalf("unexpected nsfw-only result %+v: %s", stats, env.out)
	}
}

func TestImgurRemovedPlaceholderIsSkipped(t *testing.T) {
	t.Parallel()
	// The test server can't pose as imgur.com, so exercise the check through
	// a client whose transport rewrites the final URL host.
	env := newTestEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/abcde.jpg" {
			http.Redirect(w, r, "/removed.png", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("placeholder"))
	}))
	client := env.server.Client()
	base := client.Transport
	target, _ := url.Parse(env.server.URL)
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.URL.Host = target.Host
		resp, err := base.RoundTrip(r)
		if resp != nil {
			resp.Request.URL.Host = "i.imgur.com"
		}
		return resp, err
	})
	d := env.downloader(Options{})
	d.HTTP = client

	stats := run(t, d, imagePost("i", "imgur", "https://i.imgur.com/abcde.jpg"))
	if stats.Downloaded != 0 || stats.Failed != 0 {
		t.Fatalf("expected the placeholder to be skipped silently, got %+v (%s)", stats, env.errOut)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFormatBytes(t *testing.T) {
	t.Parallel()
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1536: "1.5 KB", 50 << 20: "50.0 MB"} {
		if got := FormatBytes(n); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
