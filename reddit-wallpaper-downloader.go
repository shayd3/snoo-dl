package main

import (
	"fmt"
	"os"
	"io/ioutil"
	"io"
	"net/http"
	"encoding/json"
	"strings"
)

var REDDIT_URL string = "https://www.reddit.com"
var REDDIT_BASE_API string = "/api"
var WALLPAPER_SUBREDDIT string = "/r/wallpaper"

// Response struct
type Response struct {
	Data struct {
		Post []Post `json:"children"`
	}`json:"data"`
}

// Post struct
type Post struct {
	Data struct {
		Title string `json:"title"`
		Url string `json:"url"`
	}`json:"data"`
}

func main() {
	getTopWallpapers(os.Args[1], os.Args[2])
}

// timesort = [day | week | month | year | all]
// location = Path to save images
func getTopWallpapers(timesort string, location string) {
	url := REDDIT_URL + WALLPAPER_SUBREDDIT + "/top.json?t=" + timesort
	req, err := http.NewRequest(http.MethodGet, url, nil)
    if err != nil {
        panic(err)
	}
	
	req.Header.Set("User-agent", "wallpaper-downloader 0.1")

	client := http.DefaultClient
    resp, err := client.Do(req)
    if err != nil {
        panic(err)
	}
	
	defer resp.Body.Close()
    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        panic(err)
	}
	var responseObject Response
	json.Unmarshal(body, &responseObject)

	
	for i := 0; i < len(responseObject.Data.Post); i++ {
		fmt.Println(responseObject.Data.Post[i].Data.Title + " => " + responseObject.Data.Post[i].Data.Url)
		downloadFromUrl(responseObject.Data.Post[i].Data.Url, responseObject.Data.Post[i].Data.Title, location)
	}
}

func downloadFromUrl(url string, title string, location string) {
	tokens := strings.Split(url, ".")
	fileName := title + "." + tokens[len(tokens)-1]
	fmt.Println("Downloading", url, "to", fileName)

	// check if file exists
	if _, err := os.Stat(location + fileName); os.IsNotExist(err) {	

		// Create file
		output, err := os.Create(location + fileName)
		if err != nil {
			fmt.Println("Error while creating", fileName, "-", err)
			return
		}
		defer output.Close()

		// Get bytes
		response, err := http.Get(url)
		if err != nil {
			fmt.Println("Error while downloading", url, "-", err)
			return
		}
		defer response.Body.Close()

		// Copy to file
		n, err := io.Copy(output, response.Body)
		if err != nil {
			fmt.Println("Error while downloading", url, "-", err)
			return
		}
		fmt.Println(n, "bytes downloaded.")
	}
}