package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/shayd3/reddit-image-scraper/models"
	"github.com/spf13/cobra"
)
var (
	REDDIT_URL string = "https://www.reddit.com/r"
	VALID_TOP_PERIODS string = "day|week|month|year|all"


	DEFAULT_TOP_PERIOD string = "week"
	DEFAULT_LOCATION string = "./"
	DEFAULT_RESOLUTION_FILTER string = ""
	TOP_PERIOD string = DEFAULT_TOP_PERIOD
	SUBREDDIT string = "wallpapers"

)

// downloadCmd represents the download command
var downloadCmd = &cobra.Command{
	Use:   "download {SUBREDDIT} [day|week(default)|month|year|all]",
	Short: "Download images from a specified subreddit",
	Long: `download - will download all images from the specific subreddit.
	Default: TOP_PERIOD=week, SUBREDDIT=wallpapers`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 2 || len(args) == 0 {
			return errors.New("invalid arguments")
		}
		// Check if both arguments are provided
		if len(args) == 2 {
			var re = regexp.MustCompile(VALID_TOP_PERIODS)
			if(!re.MatchString(args[1])) {
				return fmt.Errorf("provided TOP_PERIOD was invalid. Valid periods are: %s", VALID_TOP_PERIODS)
			}
		}
		
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		SUBREDDIT = args[0]

		if(len(args) == 2) {
			TOP_PERIOD = args[1]
		}

		location, _ := cmd.Flags().GetString("location")
		resolution, _ := cmd.Flags().GetString("resolution")
		aspectRatio, _ := cmd.Flags().GetString("aspect-ratio")
		filter := parseFilters(resolution, aspectRatio)

		if(location != "") {
			getTopWallpapers(SUBREDDIT, TOP_PERIOD, filter, location)
		} else {
			getTopWallpapers(SUBREDDIT, TOP_PERIOD, filter, DEFAULT_LOCATION)
		}
		
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)
	downloadCmd.Flags().StringP("location", "l", DEFAULT_LOCATION, "location to download scrapped images")
	downloadCmd.Flags().StringP("resolution", "r", "", "only download images with specified resolution (i.e. 1920x1080)")
	downloadCmd.Flags().StringP("aspect-ratio", "a", "", "only download images that meet specified aspect ratio (i.e. 16:9)")
}

func parseFilters(resolution string, aspectRatio string) models.Filter {
	filter := models.Filter{}
	if resolution != "" {
		resolution = strings.ReplaceAll(resolution, " ", "")
		resolutionDimensions := strings.Split(resolution, "x")
		width, err := strconv.Atoi(resolutionDimensions[0])
		if err != nil {
			panic(err)
		}
		height, err := strconv.Atoi(resolutionDimensions[1])
		if err != nil {
			panic(err)
		}
		filter.ResolutionWidth = width
		filter.ResolutionHeight = height
	}

	if aspectRatio != "" {
		aspectRatio = strings.ReplaceAll(aspectRatio, " ", "")
		aspectRatioVals := strings.Split(aspectRatio, ":")
		width, err := strconv.Atoi(aspectRatioVals[0])
		if err != nil {
			panic(err)
		}
		height, err := strconv.Atoi(aspectRatioVals[1])
		if err != nil {
			panic(err)
		}
		filter.AspectRatioWidth = width
		filter.AspectRatioHeight = height
	}
	return filter
}

// timesort = [day | week | month | year | all]
// location = Path to save images
func getTopWallpapers(subreddit string, timesort string, filter models.Filter, location string) {
	url := fmt.Sprintf("%s/%s/top.json?t=%s", REDDIT_URL, subreddit, timesort)
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
	var posts = responseObject.Data.Post

	for _, post := range posts {
		var title = post.Data.Title
		var url =  post.Data.Url
		fmt.Println(title + " => " + url)
		downloadFromUrl(url, title, location)
	}
}

func downloadFromUrl(url string, title string, location string) {
	tokens := strings.Split(url, ".")
	fileName := title + "." + tokens[len(tokens)-1]
	fmt.Println("Downloading", url, "to", fileName)
	
	// add trailing slash if doesn't already exist
	if(location[len(location)-1:] != "/") {
		location = location + "/"
	}
	
	// create directory location if doesn't exist
	err := os.MkdirAll(location, os.ModePerm)
	if err != nil {
		panic(err)
	}

	// Get bytes
	response, err := http.Get(url)
	if err != nil {
		fmt.Println("Error while downloading", url, "-", err)
		return
	}
	defer response.Body.Close()
	
	// check if file exists
	if _, err := os.Stat(location + fileName); os.IsNotExist(err) {	
		// Create file
		output, err := os.Create(location + fileName)
		if err != nil {
			fmt.Println("Error while creating", fileName, "-", err)
			return
		}
		defer output.Close()

		// Copy to file
		n, err := io.Copy(output, response.Body)
		if err != nil {
			fmt.Println("Error while downloading", url, "-", err)
			return
		}
		fmt.Println(n, "bytes downloaded.")
	}
}