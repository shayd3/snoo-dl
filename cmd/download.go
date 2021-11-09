package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/shayd3/reddit-image-scraper/models"
	"github.com/spf13/cobra"
)
var (
	REDDIT_URL string = "https://www.reddit.com/r"
	WALLPAPER_SUBREDDIT string = "wallpaper"

	TOP_PERIOD string = "week"
	SUBREDDIT string = "wallpapers"

)

// downloadCmd represents the download command
var downloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Download images from a specified subreddit",
	Long: `download - will download all images from the specific subreddit.
	Default: TOP_PERIOD=week, SUBREDDIT=wallpapers`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 2 {
			return errors.New("requires 2 args. ")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Args: " + strings.Join(args, " "))
		topPeriod := args[0]
		subreddit := args[1]
		getTopWallpapers(topPeriod, subreddit)
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)
}


// timesort = [day | week | month | year | all]
// location = Path to save images
func getTopWallpapers(timesort string, location string) {
	url := fmt.Sprintf("%s/%s/top.json?t=%s", REDDIT_URL, location, timesort)
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
	var responseObject models.Response
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