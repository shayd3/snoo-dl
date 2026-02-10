package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/shayd3/snoo-dl/models"
	"github.com/spf13/cobra"
)

var (
	redditURL = "https://www.reddit.com/r"

	defaultTopPeriod = "week"
	defaultLocation  = "./"

	httpClient = &http.Client{
		Timeout: 30 * time.Second,
	}

	validTopPeriods = map[string]struct{}{
		"day":   {},
		"week":  {},
		"month": {},
		"year":  {},
		"all":   {},
	}

	supportedImageExtensions = map[string]struct{}{
		".jpg":  {},
		".jpeg": {},
		".png":  {},
		".webp": {},
		".gif":  {},
	}
)

// downloadCmd represents the download command
var downloadCmd = &cobra.Command{
	Use:   "download {SUBREDDIT} [day|week(default)|month|year|all]",
	Short: "Download images from a specified subreddit",
	Long: `download - will download all images from the specific subreddit.
	Default: TOP_PERIOD=week, SUBREDDIT=wallpapers`,
	Args: func(_ *cobra.Command, args []string) error {
		if len(args) > 2 || len(args) == 0 {
			return errors.New("invalid arguments")
		}

		if len(args) == 2 {
			if !isValidTopPeriod(args[1]) {
				return errors.New("provided TOP_PERIOD was invalid. Valid periods are: day|week|month|year|all")
			}
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		subreddit := args[0]
		topPeriod := defaultTopPeriod
		if len(args) == 2 {
			topPeriod = strings.ToLower(args[1])
		}

		location, _ := cmd.Flags().GetString("location")
		resolution, _ := cmd.Flags().GetString("resolution")
		aspectRatio, _ := cmd.Flags().GetString("aspect-ratio")
		filter, err := parseFilters(resolution, aspectRatio)
		if err != nil {
			return err
		}

		if location != "" {
			return getTopWallpapers(subreddit, topPeriod, filter, location)
		}

		return getTopWallpapers(subreddit, topPeriod, filter, defaultLocation)
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)
	downloadCmd.Flags().StringP("location", "l", defaultLocation, "location to download scraped images")
	downloadCmd.Flags().StringP("resolution", "r", "", "only download images with specified resolution (i.e. 1920x1080)")
	downloadCmd.Flags().StringP("aspect-ratio", "a", "", "only download images that meet specified aspect ratio (i.e. 16:9)")
}

func parseFilters(resolution string, aspectRatio string) (models.Filter, error) {
	filter := models.Filter{}
	if resolution != "" {
		width, height, err := parsePairValue(resolution, "x", "resolution")
		if err != nil {
			return filter, err
		}
		filter.ResolutionWidth = width
		filter.ResolutionHeight = height
	}

	if aspectRatio != "" {
		width, height, err := parsePairValue(aspectRatio, ":", "aspect-ratio")
		if err != nil {
			return filter, err
		}
		filter.AspectRatioWidth = width
		filter.AspectRatioHeight = height
	}

	return filter, nil
}

// timesort = [day | week | month | year | all]
// location = Path to save images
func getTopWallpapers(subreddit string, timesort string, filter models.Filter, location string) error {
	requestURL := fmt.Sprintf("%s/%s/top.json?t=%s", redditURL, subreddit, timesort)
	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-agent", "snoo-dl/0.1")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("reddit request failed with status %s", resp.Status)
	}

	var responseObject models.Response
	if err := json.NewDecoder(resp.Body).Decode(&responseObject); err != nil {
		return err
	}

	posts := responseObject.Data.Post

	for _, post := range posts {
		canDownload := true
		title := post.Data.Title
		postURL := post.Data.Url

		if (models.Filter{}) != filter {
			canDownload = false
			if len(post.Data.Preview.Images) != 0 {
				width := post.Data.Preview.Images[0].Source.Width
				height := post.Data.Preview.Images[0].Source.Height

				hasResolutionFilter := filter.ResolutionWidth > 0 && filter.ResolutionHeight > 0
				hasAspectRatioFilter := filter.AspectRatioWidth > 0 && filter.AspectRatioHeight > 0

				resolutionMatch := !hasResolutionFilter || (height == filter.ResolutionHeight && width == filter.ResolutionWidth)
				aspectRatioMatch := !hasAspectRatioFilter || (width*filter.AspectRatioHeight == height*filter.AspectRatioWidth)

				if resolutionMatch && aspectRatioMatch {
					canDownload = true
				}
			}
		}

		if !canDownload || !hasSupportedImageExtension(postURL) {
			continue
		}

		fmt.Println(title + " => " + postURL)
		if err := downloadFromURL(postURL, title, location); err != nil {
			fmt.Println("skipping download:", err)
		}
	}

	return nil
}

func downloadFromURL(downloadURL string, title string, location string) error {
	fileExt := imageExtension(downloadURL)
	fileName := fmt.Sprintf("%s%s", sanitizeFilename(title), fileExt)
	fmt.Println("Downloading", downloadURL, "to", fileName)

	if location == "" {
		location = defaultLocation
	}

	if err := os.MkdirAll(location, os.ModePerm); err != nil {
		return err
	}

	path := filepath.Join(location, fileName)
	if _, err := os.Stat(path); err == nil {
		fmt.Println("File already exists, skipping:", path)
		return nil
	}

	response, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("error while downloading %s - %w", downloadURL, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %s", response.Status)
	}

	output, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("error while creating %s - %w", fileName, err)
	}
	defer output.Close()

	n, err := io.Copy(output, response.Body)
	if err != nil {
		return fmt.Errorf("error while downloading %s - %w", downloadURL, err)
	}
	fmt.Println(n, "bytes downloaded.")

	return nil
}

func parsePairValue(raw string, separator string, fieldName string) (int, int, error) {
	sanitized := strings.ReplaceAll(raw, " ", "")
	parts := strings.Split(sanitized, separator)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid %s format: expected width%sheight", fieldName, separator)
	}

	left, err := strconv.Atoi(parts[0])
	if err != nil || left <= 0 {
		return 0, 0, fmt.Errorf("invalid %s width value", fieldName)
	}

	right, err := strconv.Atoi(parts[1])
	if err != nil || right <= 0 {
		return 0, 0, fmt.Errorf("invalid %s height value", fieldName)
	}

	return left, right, nil
}

func isValidTopPeriod(value string) bool {
	_, ok := validTopPeriods[strings.ToLower(value)]
	return ok
}

func hasSupportedImageExtension(rawURL string) bool {
	_, ok := supportedImageExtensions[imageExtension(rawURL)]
	return ok
}

func imageExtension(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return ".jpg"
	}

	ext := strings.ToLower(path.Ext(parsedURL.Path))
	if ext == "" {
		return ".jpg"
	}

	if _, ok := supportedImageExtensions[ext]; !ok {
		return ".jpg"
	}

	return ext
}

func sanitizeFilename(name string) string {
	if name == "" {
		return "reddit_image"
	}

	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune('_')
		default:
			b.WriteRune('_')
		}
	}

	clean := strings.Trim(b.String(), "._")
	if clean == "" {
		return "reddit_image"
	}

	return clean
}
