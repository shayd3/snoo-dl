# Reddit Image Scraper
Scrape all images from any subreddit!!


# Requirements
* GoLang

# How to run
* Clone the repo
* cd into the cloned repo
* type: `$ go install`
* type: `$ snoo-dl help`

# Deploying
There is a github actions workflow located at `.github/workflows/release_build.yml`. This uses `goreleaser` to build
and deploy the golang application. To run the release, just push up a new tag!

`git tag <version name>`
`git push --tags`

The action should run and create a new release with all of the related artifacts associated.
hi
