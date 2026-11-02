package downloader

import (
	"html"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/shayd3/snoo-dl/internal/reddit"
)

// Candidate is an image URL found in a post.
type Candidate struct {
	URL    string
	Width  int
	Height int
	// Index is the 1-based position in a gallery, or 0 for a single image.
	Index int
}

var (
	supportedImageExtensions = map[string]struct{}{
		".jpg":  {},
		".jpeg": {},
		".png":  {},
		".webp": {},
		".gif":  {},
	}

	imgurIDPattern = regexp.MustCompile(`^/([A-Za-z0-9]{5,10})$`)
)

// ExtractCandidates returns the downloadable image URLs of a post. Gallery
// posts yield one candidate per valid gallery item; other posts yield at most
// one. Crossposts with no image of their own fall back to the original post.
func ExtractCandidates(post reddit.PostData) []Candidate {
	candidates := extract(post)
	if len(candidates) == 0 {
		for _, parent := range post.CrosspostParentList {
			if candidates = extract(parent); len(candidates) > 0 {
				break
			}
		}
	}
	return candidates
}

func extract(post reddit.PostData) []Candidate {
	if post.IsGallery || len(post.GalleryData.Items) > 0 {
		return galleryCandidates(post)
	}

	width, height := 0, 0
	if len(post.Preview.Images) > 0 {
		width = post.Preview.Images[0].Source.Width
		height = post.Preview.Images[0].Source.Height
	}

	for _, raw := range []string{post.URLOverriddenByDest, post.URL} {
		if u, ok := normalizeImageURL(raw); ok {
			return []Candidate{{URL: u, Width: width, Height: height}}
		}
	}
	return nil
}

func galleryCandidates(post reddit.PostData) []Candidate {
	out := make([]Candidate, 0, len(post.GalleryData.Items))
	seen := make(map[string]struct{}, len(post.GalleryData.Items))
	for i, item := range post.GalleryData.Items {
		meta, ok := post.MediaMetadata[item.MediaID]
		if !ok || (meta.Status != "" && meta.Status != "valid") {
			continue
		}
		raw := meta.S.U
		if meta.E == "AnimatedImage" || raw == "" {
			raw = meta.S.Gif
		}
		u, ok := normalizeImageURL(raw)
		if !ok {
			continue
		}
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, Candidate{URL: u, Width: meta.S.X, Height: meta.S.Y, Index: i + 1})
	}
	return out
}

// normalizeImageURL unescapes a URL from the Reddit API, rewrites known hosts
// to their original-quality image URL, and reports whether the result is a
// supported image.
func normalizeImageURL(raw string) (string, bool) {
	unescaped := html.UnescapeString(strings.TrimSpace(raw))
	if unescaped == "" {
		return "", false
	}
	parsed, err := url.Parse(unescaped)
	if err != nil || parsed.Host == "" {
		return "", false
	}

	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "preview.redd.it":
		// Gallery sources point at preview.redd.it with resize parameters;
		// the same path on i.redd.it is the original upload.
		parsed = &url.URL{Scheme: "https", Host: "i.redd.it", Path: parsed.Path}
	case "imgur.com", "www.imgur.com", "m.imgur.com", "i.imgur.com":
		parsed = normalizeImgur(parsed)
	}

	if parsed == nil || imageExtension(parsed) == "" {
		return "", false
	}
	return parsed.String(), true
}

// normalizeImgur turns imgur page links (imgur.com/abc123) into direct image
// links and .gifv links into .gif. Albums and galleries are not supported and
// return nil.
func normalizeImgur(u *url.URL) *url.URL {
	p := u.Path
	switch ext := strings.ToLower(path.Ext(p)); ext {
	case ".gifv":
		return &url.URL{Scheme: "https", Host: "i.imgur.com", Path: strings.TrimSuffix(p, path.Ext(p)) + ".gif"}
	case "":
		if m := imgurIDPattern.FindStringSubmatch(p); m != nil {
			// i.imgur.com serves the image for any image extension.
			return &url.URL{Scheme: "https", Host: "i.imgur.com", Path: "/" + m[1] + ".jpg"}
		}
		return nil
	default:
		return u
	}
}

// imageExtension returns the supported image extension of u, from its path
// or its "format" query parameter, or "" if there is none.
func imageExtension(u *url.URL) string {
	ext := strings.ToLower(path.Ext(u.Path))
	if ext == ".jpeg" {
		return ".jpg"
	}
	if _, ok := supportedImageExtensions[ext]; ok {
		return ext
	}

	format := strings.ToLower(u.Query().Get("format"))
	if format != "" {
		if !strings.HasPrefix(format, ".") {
			format = "." + format
		}
		if _, ok := supportedImageExtensions[format]; ok {
			return format
		}
	}
	return ""
}
