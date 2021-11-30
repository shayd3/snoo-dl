package models

// Post is a struct representing a reddit post
type Post struct {
	Data struct {
		Title string `json:"title"`
		Url string `json:"url"`
	}`json:"data"`
}