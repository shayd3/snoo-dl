package downloader

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Filter restricts which images are downloaded by their dimensions. A zero
// field means "no constraint".
type Filter struct {
	// Width and Height require an exact resolution.
	Width  int
	Height int

	// AspectWidth and AspectHeight require an aspect ratio, e.g. 16 and 9.
	AspectWidth  int
	AspectHeight int
	// AspectTolerance is the allowed relative difference from the aspect
	// ratio, e.g. 0.02 for 2%. Zero means the ratio must match exactly.
	AspectTolerance float64

	// MinWidth and MinHeight require at least this many pixels.
	MinWidth  int
	MinHeight int
}

// IsZero reports whether the filter has no constraints.
func (f Filter) IsZero() bool { return f == Filter{} }

// Matches reports whether an image of the given size passes the filter.
// Images with unknown dimensions only pass an empty filter.
func (f Filter) Matches(width, height int) bool {
	if f.IsZero() {
		return true
	}
	if width <= 0 || height <= 0 {
		return false
	}

	if f.Width > 0 && f.Height > 0 && (width != f.Width || height != f.Height) {
		return false
	}
	if width < f.MinWidth || height < f.MinHeight {
		return false
	}
	if f.AspectWidth > 0 && f.AspectHeight > 0 {
		if f.AspectTolerance <= 0 {
			if width*f.AspectHeight != height*f.AspectWidth {
				return false
			}
		} else {
			want := float64(f.AspectWidth) / float64(f.AspectHeight)
			got := float64(width) / float64(height)
			if math.Abs(got-want)/want > f.AspectTolerance {
				return false
			}
		}
	}
	return true
}

// ParsePair parses values like "1920x1080" or "16:9".
func ParsePair(raw string, separator string, fieldName string) (int, int, error) {
	sanitized := strings.ReplaceAll(raw, " ", "")
	parts := strings.Split(sanitized, separator)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid %s format: expected width%sheight", fieldName, separator)
	}

	left, err := strconv.Atoi(parts[0])
	if err != nil || left <= 0 {
		return 0, 0, fmt.Errorf("invalid %s width value", fieldName)
	}

	right, err := strconv.Atoi(parts[1])
	if err != nil || right <= 0 {
		return 0, 0, fmt.Errorf("invalid %s height value", fieldName)
	}

	return left, right, nil
}
