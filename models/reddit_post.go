package models

// Post is a struct representing a reddit post
type Post struct {
	Data struct {
		Title   string `json:"title"`
		Url     string `json:"url"`
		Preview struct {
			Images []struct {
				Source struct {
					Height int    `json:"height"`
					URL    string `json:"url"`
					Width  int    `json:"width"`
				} `json:"source"`
			} `json:"images"`
		} `json:"preview"`
	} `json:"data"`
}
