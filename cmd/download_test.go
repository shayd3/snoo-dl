package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shayd3/snoo-dl/internal/reddit"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// runCLI executes the root command with args against a fake Reddit server
// and returns stdout, stderr and the error.
func runCLI(t *testing.T, server *httptest.Server, args ...string) (string, string, error) {
	t.Helper()

	origBase, origAPI, origImage := redditBaseURL, apiHTTPClient, imageClient
	redditBaseURL = server.URL
	apiHTTPClient = server.Client
	imageClient = server.Client
	t.Cleanup(func() {
		redditBaseURL, apiHTTPClient, imageClient = origBase, origAPI, origImage
	})

	// Cobra keeps flag values between executions, so reset them.
	downloadCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	cfgFile = ""
	t.Setenv("HOME", t.TempDir())

	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return out.String(), errOut.String(), err
}

type fakeReddit struct {
	t        *testing.T
	server   *httptest.Server
	requests []string
}

func newFakeReddit(t *testing.T) *fakeReddit {
	t.Helper()
	f := &fakeReddit{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("/img/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("test-image"))
	})
	mux.HandleFunc("/r/", func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.RequestURI())
		var out reddit.Listing
		img := func(id string) reddit.Post {
			return reddit.Post{Data: reddit.PostData{ID: id, Title: id, URLOverriddenByDest: f.server.URL + "/img/" + id + ".jpg"}}
		}
		switch r.URL.Path {
		case "/r/test/top.json", "/r/test/hot.json":
			switch r.URL.Query().Get("after") {
			case "":
				out.Data.Children = []reddit.Post{
					{Data: reddit.PostData{ID: "first", Title: "first", URLOverriddenByDest: "https://example.com/invalid"}},
					img("second"),
				}
				out.Data.After = "page2"
			case "page2":
				out.Data.Children = []reddit.Post{img("third")}
				out.Data.After = "page3"
			default:
				out.Data.Children = []reddit.Post{img("fourth")}
			}
		case "/r/other/top.json":
			out.Data.Children = []reddit.Post{img("other")}
		case "/r/broken/top.json":
			w.WriteHeader(http.StatusForbidden)
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	f.server = httptest.NewTLSServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestDownloadPaginatesAndHonorsLimit(t *testing.T) {
	f := newFakeReddit(t)
	dir := t.TempDir()

	out, errOut, err := runCLI(t, f.server, "download", "r/test", "month", "--limit", "3", "-l", dir)
	if err != nil {
		t.Fatalf("unexpected error %v\n%s", err, errOut)
	}
	files := listFiles(t, dir)
	if strings.Join(files, ",") != "second_second.jpg,third_third.jpg" {
		t.Fatalf("unexpected files %v", files)
	}
	if len(f.requests) != 2 {
		t.Fatalf("expected 2 listing requests, got %v", f.requests)
	}
	if !strings.Contains(f.requests[0], "t=month") || !strings.Contains(f.requests[0], "limit=3") || !strings.Contains(f.requests[1], "limit=1") {
		t.Fatalf("unexpected requests %v", f.requests)
	}
	if !strings.Contains(out, "Done: 2 downloaded") {
		t.Fatalf("missing summary in output:\n%s", out)
	}
}

func TestDownloadMultipleSubredditsAndSort(t *testing.T) {
	f := newFakeReddit(t)
	dir := t.TempDir()

	_, errOut, err := runCLI(t, f.server, "download", "test", "other", "--sort", "hot", "--limit", "1", "-l", dir, "-q")
	if err == nil {
		// r/other has no hot listing in the fake server, so it 404s.
		t.Fatalf("expected an error for the missing listing\n%s", errOut)
	}
	if !strings.Contains(errOut, "r/other: subreddit not found") {
		t.Fatalf("unexpected stderr %q", errOut)
	}
	if !strings.Contains(f.requests[0], "/r/test/hot.json") || strings.Contains(f.requests[0], "&t=") {
		t.Fatalf("unexpected request %v", f.requests)
	}
}

func TestDownloadFailureExitsNonZero(t *testing.T) {
	f := newFakeReddit(t)
	dir := t.TempDir()

	out, errOut, err := runCLI(t, f.server, "download", "broken", "other", "-l", dir)
	if err == nil || !strings.Contains(err.Error(), "1 download(s) or request(s) failed") {
		t.Fatalf("expected a failure error, got %v", err)
	}
	if !strings.Contains(errOut, "private, quarantined or banned") {
		t.Fatalf("unexpected stderr %q", errOut)
	}
	if files := listFiles(t, dir); len(files) != 1 {
		t.Fatalf("expected r/other to still be downloaded, got %v", files)
	}
	if !strings.Contains(out, "1 failed") {
		t.Fatalf("unexpected summary %q", out)
	}
}

func TestDownloadDryRun(t *testing.T) {
	f := newFakeReddit(t)
	dir := filepath.Join(t.TempDir(), "not-created")

	out, _, err := runCLI(t, f.server, "download", "other", "--dry-run", "-l", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Would download") || !strings.Contains(out, "1 image(s) would be downloaded") {
		t.Fatalf("unexpected output %q", out)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("dry run should not create the download directory")
	}
}

func TestDownloadReadsConfigFile(t *testing.T) {
	f := newFakeReddit(t)
	dir := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "snoodl.yaml")
	if err := os.WriteFile(cfg, []byte("location: "+dir+"\nlimit: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, errOut, err := runCLI(t, f.server, "--config", cfg, "download", "test")
	if err != nil {
		t.Fatalf("unexpected error %v\n%s", err, errOut)
	}
	if files := listFiles(t, dir); len(files) != 1 {
		t.Fatalf("expected config location and limit to apply, got %v", files)
	}
	// Drop the config file's values so later tests see flag defaults again.
	viper.Reset()
	if err := viper.BindPFlags(downloadCmd.Flags()); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadArgValidation(t *testing.T) {
	f := newFakeReddit(t)
	cases := map[string][]string{
		"no args":            {"download"},
		"bad subreddit":      {"download", "../../api"},
		"bad sort":           {"download", "test", "--sort", "best"},
		"bad time":           {"download", "test", "--time", "decade"},
		"bad resolution":     {"download", "test", "-r", "1920"},
		"bad aspect":         {"download", "test", "-a", "16x9"},
		"bad limit":          {"download", "test", "--limit", "0"},
		"conflicting nsfw":   {"download", "test", "--skip-nsfw", "--nsfw-only"},
		"bad concurrency":    {"download", "test", "-c", "0"},
		"negative tolerance": {"download", "test", "--aspect-tolerance", "-1"},
	}
	for name, args := range cases {
		if _, _, err := runCLI(t, f.server, args...); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if len(f.requests) != 0 {
		t.Fatalf("invalid args should not reach Reddit, got %v", f.requests)
	}
}

func TestParseDownloadArgsLegacyPeriod(t *testing.T) {
	f := newFakeReddit(t)
	if _, _, err := runCLI(t, f.server, "download", "test", "Test", "all", "--dry-run", "--limit", "1"); err != nil {
		t.Fatal(err)
	}
	if len(f.requests) != 1 || !strings.Contains(f.requests[0], "t=all") {
		t.Fatalf("expected one request for the deduplicated subreddit with t=all, got %v", f.requests)
	}
}

func TestUserAgent(t *testing.T) {
	t.Cleanup(func() { viper.Set("reddit-username", "") })
	if ua := userAgent(); !strings.Contains(ua, ":snoo-dl:") || !strings.Contains(ua, "github.com/shayd3/snoo-dl") {
		t.Fatalf("unexpected user agent %q", ua)
	}
	viper.Set("reddit-username", "u/someone")
	if ua := userAgent(); !strings.HasSuffix(ua, "(by /u/someone)") {
		t.Fatalf("unexpected user agent %q", ua)
	}
}

func TestVersionCommand(t *testing.T) {
	f := newFakeReddit(t)
	out, _, err := runCLI(t, f.server, "version")
	if err != nil || !strings.HasPrefix(out, "snoo-dl dev") {
		t.Fatalf("unexpected version output %q (%v)", out, err)
	}
}
