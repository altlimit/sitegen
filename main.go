package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
	"github.com/tdewolff/minify/v2/json"
	"github.com/tdewolff/minify/v2/svg"
	"github.com/tdewolff/minify/v2/xml"
)

var (
	cmdWG   sync.WaitGroup
	version = "v0.0.6"
)

func main() {
	log.Println("sitegen ", version)
	var (
		sitePath   string
		publicPath string
		sourceDir  string
		dataDir    string
		tplDir     string
		port       string
		basePath   string
		serve      bool
		clean      bool
		isMinify   bool
		min        *minify.M
		ss         *staticServer
		sg         *SiteGen
	)
	flag.StringVar(&sitePath, "site", "./site", "Absolute or relative root site path")
	flag.StringVar(&sourceDir, "source", "src", "Source folder relative to site path")
	flag.StringVar(&dataDir, "data", "data", "Data folder relative to site path")
	flag.StringVar(&tplDir, "templates", "templates", "Template folder relative to site path")
	flag.StringVar(&publicPath, "public", "./public", "Absolute or relative public path")
	flag.StringVar(&basePath, "base", "/", "Base folder relative to public path")
	flag.BoolVar(&serve, "serve", os.Getenv("SERVE") == "1", "Start a development server and watcher")
	flag.BoolVar(&clean, "clean", os.Getenv("CLEAN") == "1", "Clean public dir before build")
	flag.BoolVar(&isMinify, "minify", os.Getenv("MINIFY") == "1", "Minify (HTML|JS|CSS)")
	flag.StringVar(&port, "port", "8888", "Port for localhost")
	flag.Parse()

	if isMinify {
		min = minify.New()
		min.AddFunc("text/css", css.Minify)
		min.AddFunc("application/js", js.Minify)
		min.AddFunc("image/svg+xml", svg.Minify)
		min.Add("text/html", &html.Minifier{
			KeepDocumentTags: true,
		})
		min.AddFuncRegexp(regexp.MustCompile("^(application|text)/(x-)?(java|ecma)script$"), js.Minify)
		min.AddFuncRegexp(regexp.MustCompile("[/+]json$"), json.Minify)
		min.AddFuncRegexp(regexp.MustCompile("[/+]xml$"), xml.Minify)
	}

	pubPath, err := filepath.Abs(publicPath)
	if err != nil {
		log.Fatalln("Error public path ", err)
	}
	if basePath == "" {
		basePath = "/"
	}
	// should be url path
	basePath = strings.ReplaceAll(basePath, "\\", "/")
	if basePath != "/" {
		basePath = "/" + strings.Trim(basePath, "/") + "/"
	}
	sg = newSiteGen(sitePath, tplDir, dataDir, sourceDir, pubPath, basePath, min, clean, serve)
	sg.buildAll()

	if sg.dev {
		ss = newStaticServer(pubPath, basePath)
		watcher, err := fsnotify.NewWatcher()
		var mu sync.Mutex
		events := make(map[string]bool)
		if err != nil {
			log.Fatalln("Watcher error", err)
		}
		defer watcher.Close()

		buildPath := func(path string) {
			pp, err := filepath.Abs(path)
			if err != nil {
				log.Println("Failed to get absolute path ", path, " error ", err)
			}
			time.Sleep(time.Millisecond * 500)
			rp := strings.Replace(pp, sg.sitePath, "", 1)
			if strings.HasPrefix(rp, string(os.PathSeparator)+sourceDir) {
				if s, ok := sg.sources[pp]; ok {
					s.reloadContent()
				}
				sg.build(pp)
				log.Println("Rebuilt: ", rp)
			} else {
				sg.buildAll()
			}
			mu.Lock()
			delete(events, path)
			mu.Unlock()
			cmdWG.Wait()
			ss.notifier <- []byte("updated")
		}

		go func() {
			for {
				select {
				case event, ok := <-watcher.Events:
					if !ok {
						return
					}
					if event.Op&fsnotify.Write == fsnotify.Write {
						if n := strings.Split(event.Name, string(os.PathSeparator)); strings.HasPrefix(n[len(n)-1], ".") {
							return
						}
						b := false
						mu.Lock()
						if _, ok := events[event.Name]; !ok {
							events[event.Name] = true
							b = true
						}
						mu.Unlock()
						if b {
							go buildPath(event.Name)
						}
					}
				case err, ok := <-watcher.Errors:
					if !ok {
						return
					}
					log.Println("error:", err)
				}
			}
		}()

		srcDir := filepath.Join(sg.sitePath, sourceDir)
		err = watcher.Add(srcDir)
		if err != nil {
			log.Fatalln("Source DIR error: ", err)
		}
		err = watcher.Add(filepath.Join(sg.sitePath, tplDir))
		if err != nil {
			log.Println("Template DIR error: ", err)
		}
		err = watcher.Add(filepath.Join(sg.sitePath, dataDir))
		if err != nil {
			log.Println("Data DIR error: ", err)
		}
		for _, folder := range folders(srcDir) {
			if err := watcher.Add(folder); err != nil {
				log.Println("Failed to watch dir: ", folder)
			}
		}

		log.Println("Serving: ", publicPath, " at ", fmt.Sprintf("http://localhost:%s%s", port, basePath))
		log.Println("Press Ctrl+C to stop")
		http.Handle("/", ss)
		log.Println(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
	}

	cmdWG.Wait()
}

func folders(dir string) []string {
	var dirs []string
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Fatalln(err)
	}

	for _, f := range files {
		if f.IsDir() {
			path := filepath.Join(dir, f.Name())
			dirs = append(dirs, path)
			dirs = append(dirs, folders(path)...)
		}
	}
	return dirs
}
