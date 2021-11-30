package models

// Response is a struct representing a response from the reddit api
type Response struct {
	Data struct {
		Post []Post `json:"children"`
	}`json:"data"`
}