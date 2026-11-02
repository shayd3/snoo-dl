# snoo-dl

`snoo-dl` is a small CLI that downloads image posts from one or more subreddits.

## Requirements

- Go `1.26+` (only needed to build from source)

## Install

```bash
go install github.com/shayd3/snoo-dl@latest
```

Or download a prebuilt binary from the [releases page](https://github.com/shayd3/snoo-dl/releases). Each release has a `checksums.txt` and a signed build provenance attestation, which you can check with:

```bash
gh attestation verify snoo-dl_<version>_<os>_<arch>.tar.gz --repo shayd3/snoo-dl
```

Run help:

```bash
snoo-dl --help
snoo-dl download --help
snoo-dl version
```

## Usage

```bash
snoo-dl download SUBREDDIT... [day|week|month|year|all] [flags]
```

Examples:

```bash
# Top posts from /r/wallpapers over the last week (the default)
snoo-dl download wallpapers

# Top posts from /r/earthporn over the last month
snoo-dl download earthporn month          # or: --time month

# Several subreddits, using the hot listing
snoo-dl download wallpapers earthporn --sort hot --limit 50

# A multireddit
snoo-dl download wallpapers+earthporn

# Download to a folder and require an exact resolution
snoo-dl download wallpapers --location ./images --resolution 1920x1080

# Roughly 16:9 images at least 2560px wide, skipping NSFW posts
snoo-dl download wallpapers --time all -a 16:9 --aspect-tolerance 0.02 --min-width 2560 --skip-nsfw

# See what would be downloaded without downloading anything
snoo-dl download wallpapers --dry-run
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `-l, --location` | `.` | Directory to save images in |
| `--limit` | `100` | Max number of posts to process per subreddit |
| `-s, --sort` | `top` | Listing: `hot`, `new`, `top`, `rising`, `controversial` |
| `-t, --time` | `week` | Time window for `top`/`controversial`: `hour`, `day`, `week`, `month`, `year`, `all` |
| `-r, --resolution` | | Exact resolution, `WIDTHxHEIGHT` (e.g. `1920x1080`) |
| `-a, --aspect-ratio` | | Aspect ratio, `W:H` (e.g. `16:9`) |
| `--aspect-tolerance` | `0` | Allowed relative difference from `--aspect-ratio` (e.g. `0.02` = 2%) |
| `--min-width`, `--min-height` | `0` | Minimum image size in pixels |
| `--min-score` | `0` | Minimum post score |
| `--skip-nsfw` / `--nsfw-only` | off | Skip, or only keep, posts marked NSFW |
| `-n, --dry-run` | off | List what would be downloaded |
| `-c, --concurrency` | `4` | Images downloaded at once |
| `--retries` | `3` | Retries for Reddit requests after rate limiting or server errors |
| `--max-size` | `50` | Skip images larger than this many MB |
| `-q, --quiet` / `-v, --verbose` | off | Less or more output |
| `--config` | `$HOME/.snoodl.yaml` | Config file |

## Configuration

Every download flag can be set in the config file using the flag name as the key, or with an environment variable prefixed with `SNOODL_` (dashes become underscores). Command-line flags win over environment variables, which win over the config file.

```yaml
# ~/.snoodl.yaml
location: ~/Pictures/wallpapers
limit: 200
aspect-ratio: "16:9"
aspect-tolerance: 0.02
skip-nsfw: true

# Optional: your Reddit username, used in the User-Agent as Reddit asks.
reddit-username: your_name

# Optional: app-only OAuth for more reliable rate limits. Create a "script"
# app at https://www.reddit.com/prefs/apps. Prefer the environment variables
# SNOODL_CLIENT_ID and SNOODL_CLIENT_SECRET over storing the secret here.
client-id: ""
client-secret: ""
```

## Behavior and notes

- Files are named `<title>_<post id>[_<gallery position>].<ext>`. Files that already exist are skipped, so re-running only fetches new images.
- Supported formats are `.jpg`, `.png`, `.webp` and `.gif`. Gallery posts download every item at original quality (`i.redd.it`, not resized previews). Imgur image links (including page links and `.gifv`) and crossposts are followed. Imgur albums, Redgifs and Reddit videos are not supported.
- Downloads go to a temporary `.part` file and are only renamed into place when complete, so an interrupted run never leaves a truncated image behind. Ctrl-C stops cleanly.
- For safety, images are only fetched over `https`, redirects are re-checked, responses that aren't images (such as HTML error pages) or exceed `--max-size` are rejected, and connections to private or loopback addresses are refused.
- Reddit rate limits are respected (`X-Ratelimit-*` and `Retry-After`), with retries and backoff on `429` and `5xx`.
- The command exits non-zero if any request or download failed, after printing a summary.

## Development

```bash
go test -race ./...
go vet ./...
golangci-lint run
```

Code layout:

- `cmd/`: CLI commands and flag wiring
- `internal/reddit/`: Reddit listing client (pagination, rate limits, OAuth)
- `internal/downloader/`: image extraction, filtering and safe downloads

## Release

The [release workflow](.github/workflows/release_build.yml) runs on every push to `main` that changes code (docs-only changes are skipped). It tags a new version using semver bump rules from commit messages, then GoReleaser builds archives, checksums and a provenance attestation:

- `feat:` => minor bump
- `fix:`, `chore:`, etc. => patch bump
- `BREAKING CHANGE` or `type!:` => major bump

```bash
# Optional local dry run before merging:
goreleaser release --snapshot --clean
```
