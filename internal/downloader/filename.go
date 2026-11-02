package downloader

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxTitleBytes = 120

var windowsReservedNames = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
	"COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {}, "COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {}, "LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
}

// FileName builds the file name for a candidate image: the sanitized post
// title, the post ID (so posts with the same title don't collide), the
// gallery position when there is one, and the extension.
func FileName(title, postID string, index int, ext string) string {
	name := SanitizeFilename(title)
	if id := SanitizeFilename(postID); postID != "" && id != "reddit_image" {
		name += "_" + id
	}
	if index > 0 {
		name += fmt.Sprintf("_%d", index)
	}
	return name + ext
}

// SanitizeFilename keeps letters, digits, '-', '_' and '.', replaces
// everything else with '_', caps the length at a UTF-8 boundary, and avoids
// names Windows reserves.
func SanitizeFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}

	clean := truncateUTF8(strings.Trim(b.String(), "._"), maxTitleBytes)
	clean = strings.TrimRight(clean, "._")
	if clean == "" {
		return "reddit_image"
	}

	base := clean
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	if _, reserved := windowsReservedNames[strings.ToUpper(base)]; reserved {
		clean = "_" + clean
	}
	return clean
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// Printable strips control characters (including ANSI escape sequences'
// ESC byte) so untrusted text can be printed to a terminal.
func Printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
