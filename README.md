# snoo-dl

`snoo-dl` is a small CLI that downloads top image posts from a subreddit.

## Requirements

- Go `1.25+`

## Install

```bash
go install github.com/shayd3/snoo-dl@latest
```

Run help:

```bash
snoo-dl --help
snoo-dl download --help
```

## Usage

```bash
snoo-dl download <subreddit> [day|week|month|year|all] [flags]
```

Examples:

```bash
# Top posts from /r/wallpapers over the last week (default period)
snoo-dl download wallpapers

# Top posts from /r/earthporn over the last month
snoo-dl download earthporn month

# Download to a folder and require exact resolution
snoo-dl download wallpapers week --location ./images --resolution 1920x1080

# Filter by aspect ratio
snoo-dl download wallpapers all --aspect-ratio 16:9

# Process up to 300 top posts (fetched with Reddit pagination)
snoo-dl download wallpapers month --limit 300
```

Flags:

- `-l, --location` download directory (default `./`)
- `--limit` max number of top posts to process (default `100`)
- `-r, --resolution` exact resolution filter, format `WIDTHxHEIGHT` (example: `1920x1080`)
- `-a, --aspect-ratio` ratio filter, format `W:H` (example: `16:9`)
- `--config` optional path to config file (`$HOME/.snoodl.yaml` by default)

## Current behavior and notes

- Top posts are fetched with pagination until `--limit` is reached or no additional pages exist.
- Image URL extraction includes direct/original post URLs and gallery media metadata (preview variants are skipped).
- Only image URLs with known supported formats are downloaded (`.jpg`, `.jpeg`, `.png`, `.webp`, `.gif`).
- Existing files are skipped.
- Invalid filter formats return a friendly error instead of crashing.
- Reddit API failures and download HTTP failures return clear errors.

## Development

Run checks:

```bash
go test ./...
go vet ./...
```

## Release

There is a GitHub Actions workflow at `/Users/ryan/code/snoo-dl/.github/workflows/release_build.yml` that auto-releases on merge/push to `main` using semver bump rules from commit messages:

- `feat:` => minor bump
- `fix:`, `chore:`, etc => patch bump
- `BREAKING CHANGE` or `type!:` => major bump

```bash
# Optional local dry-run before merging:
goreleaser release --snapshot --clean
```
