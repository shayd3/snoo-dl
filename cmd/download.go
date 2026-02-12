package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
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
	defaultLimit     = 100

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
		limit, _ := cmd.Flags().GetInt("limit")
		resolution, _ := cmd.Flags().GetString("resolution")
		aspectRatio, _ := cmd.Flags().GetString("aspect-ratio")
		filter, err := parseFilters(resolution, aspectRatio)
		if err != nil {
			return err
		}
		if limit <= 0 {
			return errors.New("limit must be greater than 0")
		}

		if location != "" {
			return getTopWallpapers(cmd.Context(), subreddit, topPeriod, filter, location, limit)
		}

		return getTopWallpapers(cmd.Context(), subreddit, topPeriod, filter, defaultLocation, limit)
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)
	downloadCmd.Flags().StringP("location", "l", defaultLocation, "location to download scraped images")
	downloadCmd.Flags().Int("limit", defaultLimit, "max number of top posts to process")
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
func getTopWallpapers(ctx context.Context, subreddit string, timesort string, filter models.Filter, location string, limit int) error {
	remaining := limit
	after := ""

	for remaining > 0 {
		pageLimit := remaining
		if pageLimit > 100 {
			pageLimit = 100
		}

		responseObject, err := fetchTopPage(ctx, subreddit, timesort, after, pageLimit)
		if err != nil {
			return err
		}

		posts := responseObject.Data.Post
		if len(posts) == 0 {
			return nil
		}

		for _, post := range posts {
			if !passesFilters(post, filter) {
				continue
			}

			postURLs := extractCandidateImageURLs(post)
			if len(postURLs) == 0 {
				continue
			}

			fmt.Println(post.Data.Title + " => " + strings.Join(postURLs, ", "))
			for i, postURL := range postURLs {
				name := post.Data.Title
				if i > 0 {
					name = fmt.Sprintf("%s_%d", post.Data.Title, i+1)
				}
				if err := downloadFromURL(ctx, postURL, name, location); err != nil {
					fmt.Println("skipping download:", err)
				}
			}
		}

		remaining -= len(posts)
		if responseObject.Data.After == "" {
			break
		}
		after = responseObject.Data.After
	}

	return nil
}

func fetchTopPage(ctx context.Context, subreddit string, timesort string, after string, limit int) (models.Response, error) {
	var responseObject models.Response

	requestURL := fmt.Sprintf("%s/%s/top.json?t=%s&limit=%d", redditURL, subreddit, timesort, limit)
	if after != "" {
		requestURL += "&after=" + url.QueryEscape(after)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return responseObject, err
	}

	req.Header.Set("User-agent", "snoo-dl/0.1")
	resp, err := httpClient.Do(req)
	if err != nil {
		return responseObject, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return responseObject, fmt.Errorf("reddit request failed with status %s", resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(&responseObject); err != nil {
		return responseObject, err
	}

	return responseObject, nil
}

func downloadFromURL(ctx context.Context, downloadURL string, title string, location string) error {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}

	response, err := httpClient.Do(req)
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

func passesFilters(post models.Post, filter models.Filter) bool {
	if (models.Filter{}) == filter {
		return true
	}

	if len(post.Data.Preview.Images) == 0 {
		return false
	}

	width := post.Data.Preview.Images[0].Source.Width
	height := post.Data.Preview.Images[0].Source.Height
	hasResolutionFilter := filter.ResolutionWidth > 0 && filter.ResolutionHeight > 0
	hasAspectRatioFilter := filter.AspectRatioWidth > 0 && filter.AspectRatioHeight > 0

	resolutionMatch := !hasResolutionFilter || (height == filter.ResolutionHeight && width == filter.ResolutionWidth)
	aspectRatioMatch := !hasAspectRatioFilter || (width*filter.AspectRatioHeight == height*filter.AspectRatioWidth)

	return resolutionMatch && aspectRatioMatch
}

func extractCandidateImageURLs(post models.Post) []string {
	candidates := make([]string, 0, 4)

	add := func(rawURL string) {
		unescaped := html.UnescapeString(strings.TrimSpace(rawURL))
		if unescaped == "" || !hasSupportedImageExtension(unescaped) {
			return
		}
		candidates = append(candidates, unescaped)
	}

	add(post.Data.URLOverriddenByDest)
	add(post.Data.Url)

	if len(post.Data.Preview.Images) > 0 {
		add(post.Data.Preview.Images[0].Source.URL)
		for _, res := range post.Data.Preview.Images[0].Resolutions {
			add(res.URL)
		}
	}

	if post.Data.IsGallery {
		for _, item := range post.Data.GalleryData.Items {
			if meta, ok := post.Data.MediaMetadata[item.MediaID]; ok {
				add(meta.S.U)
			}
		}
	}

	return uniqueStrings(candidates)
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
	ext := imageExtension(rawURL)
	_, ok := supportedImageExtensions[ext]
	return ok
}

func imageExtension(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	ext := strings.ToLower(path.Ext(parsedURL.Path))
	if _, ok := supportedImageExtensions[ext]; ok {
		return ext
	}

	queryFormat := strings.ToLower(parsedURL.Query().Get("format"))
	if queryFormat != "" {
		if !strings.HasPrefix(queryFormat, ".") {
			queryFormat = "." + queryFormat
		}
		if _, ok := supportedImageExtensions[queryFormat]; ok {
			return queryFormat
		}
	}

	return ""
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

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}

	return out
}
