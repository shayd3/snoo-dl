package cmd

import "testing"

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

	got = imageExtension("https://example.com/no-ext")
	if got != ".jpg" {
		t.Fatalf("expected .jpg fallback, got %s", got)
	}
}

func TestSanitizeFilename(t *testing.T) {
	got := sanitizeFilename("Hello /r/wallpapers: 4K?")
	want := "Hello__r_wallpapers__4K"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
