package models

// Post is a struct representing a reddit post
type Post struct {
	Kind string   `json:"kind"`
	Data PostData `json:"data"`
}

type PostData struct {
	ID                  string               `json:"id"`
	Title               string               `json:"title"`
	Url                 string               `json:"url"`
	URLOverriddenByDest string               `json:"url_overridden_by_dest"`
	PostHint            string               `json:"post_hint"`
	IsGallery           bool                 `json:"is_gallery"`
	Preview             Preview              `json:"preview"`
	GalleryData         GalleryData          `json:"gallery_data"`
	MediaMetadata       map[string]MediaMeta `json:"media_metadata"`
}

type Preview struct {
	Images []PreviewImage `json:"images"`
}

type PreviewImage struct {
	Source      ImageSource   `json:"source"`
	Resolutions []ImageSource `json:"resolutions"`
}

type ImageSource struct {
	Height int    `json:"height"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
}

type GalleryData struct {
	Items []GalleryItem `json:"items"`
}

type GalleryItem struct {
	MediaID string `json:"media_id"`
}

type MediaMeta struct {
	E string `json:"e"`
	S struct {
		U string `json:"u"`
		X int    `json:"x"`
		Y int    `json:"y"`
	} `json:"s"`
}
