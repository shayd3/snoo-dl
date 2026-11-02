// Package downloader saves images from Reddit posts to disk.
package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/shayd3/snoo-dl/internal/reddit"
)

// DefaultMaxBytes is the default size limit for one image.
const DefaultMaxBytes int64 = 50 << 20

// ErrTooLarge is returned when an image is bigger than Options.MaxBytes.
var ErrTooLarge = errors.New("image exceeds the size limit")

// Options control which posts are downloaded and how.
type Options struct {
	Location string
	Filter   Filter

	SkipNSFW bool
	NSFWOnly bool
	MinScore int

	DryRun      bool
	Concurrency int
	MaxBytes    int64

	Quiet   bool
	Verbose bool
}

// Stats summarizes a run.
type Stats struct {
	Downloaded int
	Existing   int
	Failed     int
	Planned    int // images a dry run would download
	Bytes      int64
}

// Downloader downloads images from posts using a pool of workers.
type Downloader struct {
	HTTP      *http.Client
	UserAgent string
	Options   Options
	Out       io.Writer
	Err       io.Writer

	// ValidateURL checks every image URL before it is requested. It defaults
	// to the package-level ValidateURL.
	ValidateURL func(*url.URL) error

	jobs    chan job
	wg      sync.WaitGroup
	mu      sync.Mutex
	outMu   sync.Mutex
	stats   Stats
	started bool
}

type job struct {
	title string
	url   string
	path  string
}

// Start launches the worker pool. Call Submit for each post and then Wait.
func (d *Downloader) Start(ctx context.Context) {
	if d.started {
		return
	}
	d.started = true
	if d.Out == nil {
		d.Out = io.Discard
	}
	if d.Err == nil {
		d.Err = io.Discard
	}
	if d.ValidateURL == nil {
		d.ValidateURL = ValidateURL
	}
	if d.Options.MaxBytes <= 0 {
		d.Options.MaxBytes = DefaultMaxBytes
	}
	if d.Options.Location == "" {
		d.Options.Location = "."
	}

	workers := d.Options.Concurrency
	if workers <= 0 {
		workers = 1
	}
	d.jobs = make(chan job)
	for range workers {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			for j := range d.jobs {
				d.run(ctx, j)
			}
		}()
	}
}

// Submit queues the images of a post that pass the filters. It blocks while
// all workers are busy and returns ctx.Err() if ctx is cancelled.
func (d *Downloader) Submit(ctx context.Context, post reddit.PostData) error {
	if !d.wantPost(post) {
		return nil
	}

	candidates := ExtractCandidates(post)
	matched := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		if d.Options.Filter.Matches(c.Width, c.Height) {
			matched = append(matched, c)
		}
	}
	if len(matched) == 0 {
		if d.Options.Verbose && len(candidates) > 0 {
			d.printf(d.Out, "Filtered out: %s\n", Printable(post.Title))
		}
		return nil
	}

	for _, c := range matched {
		u, err := url.Parse(c.URL)
		if err != nil {
			continue
		}
		name := FileName(post.Title, post.ID, c.Index, imageExtension(u))
		j := job{
			title: Printable(post.Title),
			url:   c.URL,
			path:  filepath.Join(d.Options.Location, name),
		}

		if d.Options.DryRun {
			d.printf(d.Out, "Would download %s => %s\n", j.url, j.path)
			d.mu.Lock()
			d.stats.Planned++
			d.mu.Unlock()
			continue
		}

		select {
		case d.jobs <- j:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Wait stops accepting jobs, waits for running downloads and returns stats.
func (d *Downloader) Wait() Stats {
	if d.started {
		close(d.jobs)
		d.wg.Wait()
		d.started = false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stats
}

// RecordFailure counts a failure that happened outside the downloader, such
// as a listing request error, so it shows up in the summary.
func (d *Downloader) RecordFailure() {
	d.mu.Lock()
	d.stats.Failed++
	d.mu.Unlock()
}

func (d *Downloader) wantPost(post reddit.PostData) bool {
	if d.Options.SkipNSFW && post.Over18 {
		return false
	}
	if d.Options.NSFWOnly && !post.Over18 {
		return false
	}
	if post.Score < d.Options.MinScore {
		return false
	}
	return true
}

func (d *Downloader) run(ctx context.Context, j job) {
	if _, err := os.Stat(j.path); err == nil {
		if d.Options.Verbose {
			d.printf(d.Out, "Already exists, skipping: %s\n", j.path)
		}
		d.mu.Lock()
		d.stats.Existing++
		d.mu.Unlock()
		return
	}

	n, err := d.download(ctx, j.url, j.path)
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		if errors.Is(err, errSkip) {
			if d.Options.Verbose {
				d.printf(d.Err, "Skipping %s: %v\n", j.url, err)
			}
			return
		}
		d.stats.Failed++
		if ctx.Err() == nil {
			d.printf(d.Err, "Failed %s (%s): %v\n", j.title, j.url, err)
		}
		return
	}
	d.stats.Downloaded++
	d.stats.Bytes += n
	d.printf(d.Out, "Downloaded %s => %s (%s)\n", j.title, j.path, FormatBytes(n))
}

// errSkip marks responses that are not failures but shouldn't be saved, such
// as imgur's "removed" placeholder.
var errSkip = errors.New("image was removed")

// download fetches downloadURL into a temporary file next to dest and renames
// it into place only once the whole body has been written.
func (d *Downloader) download(ctx context.Context, downloadURL, dest string) (int64, error) {
	u, err := url.Parse(downloadURL)
	if err != nil {
		return 0, err
	}
	if err := d.ValidateURL(u); err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return 0, err
	}
	if d.UserAgent != "" {
		req.Header.Set("User-Agent", d.UserAgent)
	}

	resp, err := d.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %s", resp.Status)
	}
	if resp.Request != nil && strings.Contains(resp.Request.URL.Host, "imgur.com") &&
		strings.HasPrefix(resp.Request.URL.Path, "/removed.") {
		return 0, errSkip
	}
	if err := checkContentType(resp.Header.Get("Content-Type")); err != nil {
		return 0, err
	}
	if resp.ContentLength > d.Options.MaxBytes {
		return 0, fmt.Errorf("%w (%s > %s)", ErrTooLarge, FormatBytes(resp.ContentLength), FormatBytes(d.Options.MaxBytes))
	}

	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(dir, ".snoo-dl-*.part")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	n, err := io.Copy(tmp, io.LimitReader(resp.Body, d.Options.MaxBytes+1))
	if err != nil {
		return 0, err
	}
	if n > d.Options.MaxBytes {
		return 0, fmt.Errorf("%w (%s)", ErrTooLarge, FormatBytes(d.Options.MaxBytes))
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return 0, err
	}
	keep = true
	return n, nil
}

// checkContentType rejects responses that are clearly not images, such as
// HTML error pages served with status 200.
func checkContentType(raw string) error {
	if raw == "" {
		return nil
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return fmt.Errorf("invalid content type %q", raw)
	}
	if strings.HasPrefix(mediaType, "image/") || mediaType == "application/octet-stream" {
		return nil
	}
	return fmt.Errorf("unexpected content type %q", mediaType)
}

func (d *Downloader) printf(w io.Writer, format string, args ...any) {
	if d.Options.Quiet && w == d.Out {
		return
	}
	d.outMu.Lock()
	defer d.outMu.Unlock()
	fmt.Fprintf(w, format, args...)
}

// FormatBytes renders a byte count like "1.2 MB".
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
