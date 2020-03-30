# sitegen

Sitegen is a simple but flexible static site generator.

## Setup

Download the sitegen bundles from release page.

Bundled with the executables is a sample website that you can start with. Extract the zip file then run the command below.

```shell
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

- [altlimit.com](https://www.altlimit.com) - [source](https://github.com/faisalraja/altlimit)

## File Handlers

File handlers is a way to process any file differently when it changes by running a specific command. Example for using tailwind with postcss & purgecss. If you are just using cdn in your templates you don't need this. You can skip to styles.css if you just want to see how to run a command in a per file basis.

```shell
# Insta postcss tailwind and autoprefixer
npm init -y
npm install tailwindcss autoprefixer postcss-cli
npx tailwind init
```

In your postcss.config.js

```js
const purgecss = require("@fullhuman/postcss-purgecss");
const cssnano = require("cssnano");
module.exports = {
  plugins: [
    require("tailwindcss"),
    process.env.NODE_ENV === "production" ? require("autoprefixer") : null,
    process.env.NODE_ENV === "production"
      ? cssnano({ preset: "default" })
      : null,
    process.env.NODE_ENV === "production"
      ? purgecss({
          content: ["./src/**/*.html", "./templates/**/*.html"],
          defaultExtractor: content => content.match(/[\w-/:]+(?<!:)/g) || []
        })
      : null
  ]
};
```

Add these scripts in your package.json

```json
"scripts": {
    "build:css": "node_modules/postcss-cli/bin/postcss src/css/styles.css -o public/css/styles.css",
    "build:prod:css": "node_modules/postcss-cli/bin/postcss --env=production src/css/styles.css -o public/css/styles.css"
}
```

In your styles.css add below.
serve: for running while in development
build: for final build, useful for minification and purgecss

```css
/*
---
serve: npm run build:css
build: npm run build:prod:css
---
*/
@tailwind base;
@tailwind components;
@tailwind utilities;
```
