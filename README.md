# sitegen

Sitegen is a simple but flexible static site generator.

## Setup

Download the sitegen bundles from release page for windows or install in linux/osx.

```bash
# Install under unix/linux env.
curl -s -S -L https://raw.githubusercontent.com/altlimit/sitegen/master/install.sh | bash
# Restart your terminal or source your rc file.

# Create a new template project
sitegen -create

# Run sitegen with development server
sitegen -serve

# Run final build
sitegen

# Build for github pages, just add -serve to test & add  -minify to minify output
sitegen -public ./docs

# Build in dist but serve under a subdirectory blog
sitegen -public ./dist  -base /blog

# For more options
sitegen -help
```

## Built with sitegen

- [altlimit.com](https://www.altlimit.com) - [source](https://github.com/altlimit/website)
- [wikiyou.org](https://www.wikiyou.org) - [source](https://github.com/altlimit/wikiyou)
- [blog.shopswired.com](https://blog.shopswired.com/)

## File Handlers

File handlers is a way to process any file differently when it changes by running a specific command.

If you want to run npm run build:css when it's development and npm run build:prod:css for final build add this to the css file:

```css
/*
---
serve: npm run build:css
build: npm run build:prod:css
---
*/
```

## Template functions

Uses go html template.

- path - prefixes any path with base path
- sources "(Path|Local|Filename|Meta.\*)" "Pattern" - returns source array that matches pattern
- data "file.json" - loads any data under data dir
- json - converts data to json for javascript/json use
- js - no escape js
- html - no escape html
- css - no escape
- select - accepts json object that turns it into an array of {Key, Value} to allow sorting
- sort "(Path|Local|Filename|Meta.\*|Key|Value.\*|\*)" "(asc|desc) - orders sources or array from data
- limit n - limits sources or array from data
- offset n - offsets sources or array from data
- paginate n - paginate a sources or array from data while providing page limit "n", "Page" and "Pages" is populated in current page
- pages Source - returns list of pages with Path, Page & Active properties for creating links
- page "path" - creates a parametarized page, page will have .Path variable

## Page variables

All yaml meta data are in each page are available plus values below.

- Dev - true when it's -serve mode
- Source - current page source
- BasePath - base path from provided base path param (defaults to /)
- Today - current day YYYY-MM-DD at build time
- Pages|Pages - populated when "paginate" is used
- Path - populated when "page" is ued