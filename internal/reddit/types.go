package reddit

// Listing is a page of posts returned by a Reddit listing endpoint
// (for example /r/{subreddit}/top.json).
type Listing struct {
	Data ListingData `json:"data"`
}

// ListingData holds the posts of a listing page and the cursor for the next page.
type ListingData struct {
	Children []Post `json:"children"`
	After    string `json:"after"`
}

// Post is a single listing entry.
type Post struct {
	Kind string   `json:"kind"`
	Data PostData `json:"data"`
}

// PostData is the subset of a Reddit post that snoo-dl uses.
type PostData struct {
	ID                  string               `json:"id"`
	Title               string               `json:"title"`
	URL                 string               `json:"url"`
	URLOverriddenByDest string               `json:"url_overridden_by_dest"`
	PostHint            string               `json:"post_hint"`
	Over18              bool                 `json:"over_18"`
	Score               int                  `json:"score"`
	IsGallery           bool                 `json:"is_gallery"`
	Preview             Preview              `json:"preview"`
	GalleryData         GalleryData          `json:"gallery_data"`
	MediaMetadata       map[string]MediaMeta `json:"media_metadata"`
	CrosspostParentList []PostData           `json:"crosspost_parent_list"`
}

// Preview holds Reddit-generated preview images for a post.
type Preview struct {
	Images []PreviewImage `json:"images"`
}

// PreviewImage is one preview image with its source size and resized variants.
type PreviewImage struct {
	Source      ImageSource   `json:"source"`
	Resolutions []ImageSource `json:"resolutions"`
}

// ImageSource is an image URL with its dimensions.
type ImageSource struct {
	Height int    `json:"height"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
}

// GalleryData lists the items of a gallery post in display order.
type GalleryData struct {
	Items []GalleryItem `json:"items"`
}

// GalleryItem references an entry in PostData.MediaMetadata.
type GalleryItem struct {
	MediaID string `json:"media_id"`
}

// MediaMeta describes one gallery media item.
type MediaMeta struct {
	Status string      `json:"status"` // "valid" when the media is usable
	E      string      `json:"e"`      // "Image" or "AnimatedImage"
	M      string      `json:"m"`      // mime type, e.g. "image/jpg"
	S      MediaSource `json:"s"`
}

// MediaSource is the full-size source of a gallery media item. Static images
// set U; animated images set Gif and/or MP4 instead.
type MediaSource struct {
	U   string `json:"u"`
	Gif string `json:"gif"`
	MP4 string `json:"mp4"`
	X   int    `json:"x"`
	Y   int    `json:"y"`
}
