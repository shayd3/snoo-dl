package models

// Response is a struct representing a response from the reddit api
type Response struct {
	Data ListingData `json:"data"`
}

type ListingData struct {
	Post  []Post `json:"children"`
	After string `json:"after"`
}
