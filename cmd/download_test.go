package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shayd3/snoo-dl/models"
)

func TestParseFiltersValid(t *testing.T) {
	filter, err := parseFilters("1920x1080", "16:9")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if filter.ResolutionWidth != 1920 || filter.ResolutionHeight != 1080 {
		t.Fatalf("unexpected resolution filter: %+v", filter)
	}

	if filter.AspectRatioWidth != 16 || filter.AspectRatioHeight != 9 {
		t.Fatalf("unexpected aspect ratio filter: %+v", filter)
	}
}

func TestParseFiltersInvalidResolution(t *testing.T) {
	_, err := parseFilters("1920", "")
	if err == nil {
		t.Fatal("expected an error for invalid resolution format")
	}
}

func TestParseFiltersInvalidAspectRatio(t *testing.T) {
	_, err := parseFilters("", "16x9")
	if err == nil {
		t.Fatal("expected an error for invalid aspect-ratio format")
	}
}

func TestIsValidTopPeriod(t *testing.T) {
	if !isValidTopPeriod("WEEK") {
		t.Fatal("expected WEEK to be valid")
	}

	if isValidTopPeriod("weekday") {
		t.Fatal("expected weekday to be invalid")
	}
}

func TestImageExtension(t *testing.T) {
	got := imageExtension("https://i.redd.it/test.png?width=1920&format=png")
	if got != ".png" {
		t.Fatalf("expected .png, got %s", got)
	}

	got = imageExtension("https://example.com/no-ext?format=webp")
	if got != ".webp" {
		t.Fatalf("expected .webp from query format, got %s", got)
	}

	got = imageExtension("https://example.com/no-ext")
	if got != "" {
		t.Fatalf("expected empty extension for unknown format, got %s", got)
	}
}

func TestSanitizeFilename(t *testing.T) {
	got := sanitizeFilename("Hello /r/wallpapers: 4K?")
	want := "Hello__r_wallpapers__4K"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestExtractCandidateImageURLs(t *testing.T) {
	post := models.Post{
		Data: models.PostData{
			Url:                 "https://i.redd.it/from-url.jpg",
			URLOverriddenByDest: "https://i.redd.it/from-overridden.png",
			IsGallery:           true,
			GalleryData: models.GalleryData{
				Items: []models.GalleryItem{
					{MediaID: "media-1"},
				},
			},
			MediaMetadata: map[string]models.MediaMeta{
				"media-1": {
					S: struct {
						U string `json:"u"`
						X int    `json:"x"`
						Y int    `json:"y"`
					}{
						U: "https://i.redd.it/gallery.webp",
					},
				},
			},
			Preview: models.Preview{
				Images: []models.PreviewImage{
					{
						Source: models.ImageSource{
							URL:    "https://preview.redd.it/source.jpg?width=1920&amp;format=pjpg",
							Width:  1920,
							Height: 1080,
						},
					},
				},
			},
		},
	}

	got := extractCandidateImageURLs(post)
	if len(got) != 3 {
		t.Fatalf("expected 3 candidate URLs, got %d (%v)", len(got), got)
	}

	for _, candidate := range got {
		if strings.Contains(candidate.URL, "&amp;") {
			t.Fatalf("expected HTML entities to be unescaped, got %q", candidate.URL)
		}
		if strings.Contains(candidate.URL, "preview.redd.it") {
			t.Fatalf("did not expect preview URL candidate: %q", candidate.URL)
		}
	}
}

func TestFilterCandidatesByResolution(t *testing.T) {
	candidates := []imageCandidate{
		{URL: "https://i.redd.it/a.jpg", Width: 1920, Height: 1080},
		{URL: "https://i.redd.it/b.jpg", Width: 1080, Height: 1080},
	}

	filtered := filterCandidates(candidates, models.Filter{
		ResolutionWidth:  1920,
		ResolutionHeight: 1080,
	})

	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered candidate, got %d", len(filtered))
	}
	if filtered[0].URL != "https://i.redd.it/a.jpg" {
		t.Fatalf("unexpected filtered URL %q", filtered[0].URL)
	}
}

func TestGetTopWallpapersPaginatesAndHonorsLimit(t *testing.T) {
	tmpDir := t.TempDir()
	var serverURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/img/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("test-image"))
	})
	mux.HandleFunc("/r/test/top.json", func(w http.ResponseWriter, r *http.Request) {
		after := r.URL.Query().Get("after")
		limit := r.URL.Query().Get("limit")
		if limit != "3" && limit != "1" {
			t.Fatalf("unexpected page limit value %q", limit)
		}

		type response struct {
			Data struct {
				Children []models.Post `json:"children"`
				After    string        `json:"after"`
			} `json:"data"`
		}
		out := response{}

		switch after {
		case "":
			out.Data.Children = []models.Post{
				{Data: models.PostData{Title: "first", URLOverriddenByDest: "http://example.com/invalid"}},
				{Data: models.PostData{Title: "second", URLOverriddenByDest: serverURL + "/img/second.jpg"}},
			}
			out.Data.After = "page2"
		case "page2":
			out.Data.Children = []models.Post{
				{Data: models.PostData{Title: "third", URLOverriddenByDest: serverURL + "/img/third.jpg"}},
			}
			out.Data.After = "page3"
		default:
			out.Data.Children = []models.Post{
				{Data: models.PostData{Title: "fourth", URLOverriddenByDest: serverURL + "/img/fourth.jpg"}},
			}
		}

		_ = json.NewEncoder(w).Encode(out)
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL

	originalRedditURL := redditURL
	originalClient := httpClient
	redditURL = server.URL + "/r"
	httpClient = server.Client()
	defer func() {
		redditURL = originalRedditURL
		httpClient = originalClient
	}()

	if err := getTopWallpapers(context.Background(), "test", "week", models.Filter{}, tmpDir, 3); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read temp directory: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 downloaded files (invalid URL skipped), got %d", len(files))
	}

	_, err = os.Stat(filepath.Join(tmpDir, "second.jpg"))
	if err != nil {
		t.Fatalf("expected second.jpg to exist: %v", err)
	}
	_, err = os.Stat(filepath.Join(tmpDir, "third.jpg"))
	if err != nil {
		t.Fatalf("expected third.jpg to exist: %v", err)
	}
}
