# Reddit Wallpaper Downloader
First GoLang project to get a feel for the language. This will get the top posts from /r/wallpapers from reddit and will save the images that show up on the first page to your specified location. 

# Requirements
* GoLang

# How to run
* Clone the repo
* cd into the cloned repo
* type: `$ go install`
* type: `$ reddit-wallpaper-downloader [day | week | month | year | all] [path to download]`

# Example
On a windows machine:
`$ reddit-wallpaper-downloader day %HOMEPATH$\Pictures\Wallpapers\`

This will download the wallpapers for the top posts of the day to %HOMEPATH$\Pictures\Wallpapers\. Each file will take the name of the title of the post. If the file already exists, it will skip downloading that wallpaper.