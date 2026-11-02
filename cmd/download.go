package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shayd3/snoo-dl/internal/downloader"
	"github.com/shayd3/snoo-dl/internal/reddit"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	defaultSort        = "top"
	defaultTime        = "week"
	defaultLocation    = "."
	defaultLimit       = 100
	defaultConcurrency = 4
	defaultRetries     = 3
	defaultMaxSizeMB   = 50
)

// Overridden in tests.
var (
	redditBaseURL  = reddit.DefaultBaseURL
	redditTokenURL = reddit.DefaultTokenURL
	redditOAuthURL = reddit.DefaultOAuthBaseURL
	apiHTTPClient  = func() *http.Client { return &http.Client{Timeout: 30 * time.Second} }
	imageClient    = func() *http.Client { return downloader.NewHTTPClient(2 * time.Minute) }
	validateURL    = downloader.ValidateURL
)

var downloadCmd = &cobra.Command{
	Use:   "download SUBREDDIT... [day|week|month|year|all]",
	Short: "Download images from one or more subreddits",
	Long: `Download images from one or more subreddits.

By default the top posts of the past week are used. Pick a different listing
with --sort (hot, new, top, rising, controversial) and --time (hour, day,
week, month, year, all). For backwards compatibility the time window can
also be given as the last argument.

Subreddits can be written as "wallpapers", "r/wallpapers" or combined as
"wallpapers+earthporn". Files are named <title>_<post id>[_<gallery #>].<ext>,
and files that already exist are skipped.`,
	Example: `  snoo-dl download wallpapers
  snoo-dl download earthporn month
  snoo-dl download wallpapers earthporn --sort hot --limit 50
  snoo-dl download wallpapers --time all --aspect-ratio 16:9 --aspect-tolerance 0.02
  snoo-dl download wallpapers --min-width 2560 --skip-nsfw --dry-run`,
	Args: cobra.MinimumNArgs(1),
	RunE: runDownload,
}

func init() {
	rootCmd.AddCommand(downloadCmd)
	f := downloadCmd.Flags()
	f.StringP("location", "l", defaultLocation, "directory to save images in")
	f.Int("limit", defaultLimit, "max number of posts to process per subreddit")
	f.StringP("sort", "s", defaultSort, "listing to use: hot|new|top|rising|controversial")
	f.StringP("time", "t", defaultTime, "time window for top/controversial: hour|day|week|month|year|all")
	f.StringP("resolution", "r", "", "only download images with this exact resolution (e.g. 1920x1080)")
	f.StringP("aspect-ratio", "a", "", "only download images with this aspect ratio (e.g. 16:9)")
	f.Float64("aspect-tolerance", 0, "allowed relative difference from --aspect-ratio (e.g. 0.02 for 2%)")
	f.Int("min-width", 0, "only download images at least this wide")
	f.Int("min-height", 0, "only download images at least this tall")
	f.Int("min-score", 0, "only download posts with at least this score")
	f.Bool("skip-nsfw", false, "skip posts marked NSFW")
	f.Bool("nsfw-only", false, "only download posts marked NSFW")
	f.BoolP("dry-run", "n", false, "list what would be downloaded without downloading")
	f.IntP("concurrency", "c", defaultConcurrency, "number of images to download at once")
	f.Int("retries", defaultRetries, "times to retry Reddit requests after rate limiting or server errors")
	f.Int("max-size", defaultMaxSizeMB, "skip images larger than this many megabytes")
	f.BoolP("quiet", "q", false, "only print errors and the summary")
	f.BoolP("verbose", "v", false, "also print skipped and filtered posts")

	if err := viper.BindPFlags(f); err != nil {
		panic(err)
	}
}

type downloadArgs struct {
	subreddits []string
	sort       string
	period     string
}

// parseDownloadArgs splits positional args into subreddits and the optional
// legacy time window, and resolves --sort/--time.
func parseDownloadArgs(cmd *cobra.Command, args []string) (downloadArgs, error) {
	out := downloadArgs{
		sort:   strings.ToLower(viper.GetString("sort")),
		period: strings.ToLower(viper.GetString("time")),
	}

	names := args
	if len(args) >= 2 && reddit.IsValidTime(args[len(args)-1]) {
		if !cmd.Flags().Changed("time") {
			out.period = strings.ToLower(args[len(args)-1])
		}
		names = args[:len(args)-1]
	}

	if !reddit.IsValidSort(out.sort) {
		return out, fmt.Errorf("invalid --sort %q: valid values are %s", out.sort, strings.Join(reddit.Sorts, "|"))
	}
	if !reddit.IsValidTime(out.period) {
		return out, fmt.Errorf("invalid time window %q: valid values are %s", out.period, strings.Join(reddit.Times, "|"))
	}

	seen := map[string]struct{}{}
	for _, raw := range names {
		name, err := reddit.NormalizeSubreddit(raw)
		if err != nil {
			return out, err
		}
		key := strings.ToLower(name)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out.subreddits = append(out.subreddits, name)
	}
	return out, nil
}

func parseFilter() (downloader.Filter, error) {
	filter := downloader.Filter{
		AspectTolerance: viper.GetFloat64("aspect-tolerance"),
		MinWidth:        viper.GetInt("min-width"),
		MinHeight:       viper.GetInt("min-height"),
	}
	if filter.AspectTolerance < 0 || filter.MinWidth < 0 || filter.MinHeight < 0 {
		return filter, errors.New("--aspect-tolerance, --min-width and --min-height must not be negative")
	}

	if resolution := viper.GetString("resolution"); resolution != "" {
		w, h, err := downloader.ParsePair(resolution, "x", "resolution")
		if err != nil {
			return filter, err
		}
		filter.Width, filter.Height = w, h
	}
	if ratio := viper.GetString("aspect-ratio"); ratio != "" {
		w, h, err := downloader.ParsePair(ratio, ":", "aspect-ratio")
		if err != nil {
			return filter, err
		}
		filter.AspectWidth, filter.AspectHeight = w, h
	}
	return filter, nil
}

func userAgent() string {
	contact := "+https://github.com/shayd3/snoo-dl"
	if user := strings.TrimPrefix(viper.GetString("reddit-username"), "u/"); user != "" {
		contact = "by /u/" + user
	}
	return fmt.Sprintf("%s:snoo-dl:%s (%s)", runtime.GOOS, Version(), contact)
}

func runDownload(cmd *cobra.Command, args []string) error {
	parsed, err := parseDownloadArgs(cmd, args)
	if err != nil {
		return err
	}
	filter, err := parseFilter()
	if err != nil {
		return err
	}

	limit := viper.GetInt("limit")
	if limit <= 0 {
		return errors.New("--limit must be greater than 0")
	}
	if viper.GetBool("skip-nsfw") && viper.GetBool("nsfw-only") {
		return errors.New("--skip-nsfw and --nsfw-only can't be used together")
	}
	concurrency := viper.GetInt("concurrency")
	if concurrency <= 0 {
		return errors.New("--concurrency must be greater than 0")
	}
	maxSize := viper.GetInt("max-size")
	if maxSize <= 0 {
		return errors.New("--max-size must be greater than 0")
	}
	location := viper.GetString("location")
	if location == "" {
		location = defaultLocation
	}
	if location == "~" || strings.HasPrefix(location, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			location = filepath.Join(home, strings.TrimPrefix(location, "~"))
		}
	}

	ua := userAgent()
	client := reddit.New(apiHTTPClient(), ua)
	client.BaseURL = redditBaseURL
	client.TokenURL = redditTokenURL
	client.OAuthBaseURL = redditOAuthURL
	client.MaxRetries = max(viper.GetInt("retries"), 0)
	client.ClientID = viper.GetString("client-id")
	client.ClientSecret = viper.GetString("client-secret")

	dl := &downloader.Downloader{
		HTTP:        imageClient(),
		UserAgent:   ua,
		Out:         cmd.OutOrStdout(),
		Err:         cmd.ErrOrStderr(),
		ValidateURL: validateURL,
		Options: downloader.Options{
			Location:    location,
			Filter:      filter,
			SkipNSFW:    viper.GetBool("skip-nsfw"),
			NSFWOnly:    viper.GetBool("nsfw-only"),
			MinScore:    viper.GetInt("min-score"),
			DryRun:      viper.GetBool("dry-run"),
			Concurrency: concurrency,
			MaxBytes:    int64(maxSize) << 20,
			Quiet:       viper.GetBool("quiet"),
			Verbose:     viper.GetBool("verbose"),
		},
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	dl.Start(ctx)

	var fetchErr error
	for _, subreddit := range parsed.subreddits {
		if err := fetchSubreddit(ctx, client, dl, subreddit, parsed.sort, parsed.period, limit); err != nil {
			if errors.Is(err, context.Canceled) {
				fetchErr = err
				break
			}
			dl.RecordFailure()
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
		}
	}
	stats := dl.Wait()

	printSummary(cmd.OutOrStdout(), stats, dl.Options.DryRun)
	if fetchErr != nil {
		return fetchErr
	}
	if stats.Failed > 0 {
		return fmt.Errorf("%d download(s) or request(s) failed", stats.Failed)
	}
	return nil
}

// fetchSubreddit pages through a listing until limit posts have been seen or
// there are no more pages, submitting each post to the downloader. Listing
// requests stay serial so they respect Reddit's rate limit.
func fetchSubreddit(ctx context.Context, client *reddit.Client, dl *downloader.Downloader, subreddit, sort, period string, limit int) error {
	remaining := limit
	after := ""
	for remaining > 0 {
		listing, err := client.Listing(ctx, subreddit, reddit.ListingOptions{
			Sort:  sort,
			Time:  period,
			After: after,
			Limit: min(remaining, reddit.MaxPageSize),
		})
		if err != nil {
			return err
		}

		posts := listing.Data.Children
		if len(posts) == 0 {
			return nil
		}
		for _, post := range posts {
			if err := dl.Submit(ctx, post.Data); err != nil {
				return err
			}
		}

		remaining -= len(posts)
		if listing.Data.After == "" {
			return nil
		}
		after = listing.Data.After
	}
	return nil
}

func printSummary(w io.Writer, stats downloader.Stats, dryRun bool) {
	if dryRun {
		fmt.Fprintf(w, "Dry run: %d image(s) would be downloaded, %d failed.\n", stats.Planned, stats.Failed)
		return
	}
	fmt.Fprintf(w, "Done: %d downloaded (%s), %d already existed, %d failed.\n",
		stats.Downloaded, downloader.FormatBytes(stats.Bytes), stats.Existing, stats.Failed)
}
