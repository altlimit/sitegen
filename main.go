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
	templateDir string
	dataDir     string
	serving     bool
	withMinify  bool
	min         *minify.M
	cmdWG       sync.WaitGroup
)

func main() {
	var (
		publicDir string
		sourceDir string
		port      string
		clean     bool
		ss        *staticServer
	)
	flag.StringVar(&publicDir, "public", "./public", "Public directory")
	flag.StringVar(&sourceDir, "source", "./src", "Source directory")
	flag.StringVar(&dataDir, "data", "./data", "Data directory")
	flag.StringVar(&templateDir, "templates", "./templates", "Template directory")
	flag.BoolVar(&serving, "serve", os.Getenv("SERVE") == "1", "Watch for changes & serve")
	flag.BoolVar(&clean, "clean", os.Getenv("CLEAN") == "1", "Clean public dir before build")
	flag.BoolVar(&withMinify, "minify", os.Getenv("MINIFY") == "1", "Minify (HTML|JS|CSS)")
	flag.StringVar(&port, "port", "8888", "Port for localhost")
	flag.Parse()

	if err := os.RemoveAll(publicDir); err != nil {
		log.Fatalln(err)
	}

	if withMinify {
		min = minify.New()
		min.AddFunc("text/css", css.Minify)
		min.AddFunc("application/js", js.Minify)
		min.AddFunc("text/html", html.Minify)
		min.AddFunc("image/svg+xml", svg.Minify)
		min.AddFuncRegexp(regexp.MustCompile("^(application|text)/(x-)?(java|ecma)script$"), js.Minify)
		min.AddFuncRegexp(regexp.MustCompile("[/+]json$"), json.Minify)
		min.AddFuncRegexp(regexp.MustCompile("[/+]xml$"), xml.Minify)
	}

	allSources := make(map[string]Source)

	build := func(p string) {
		out := make(map[string]int)
		baseSource, err := loadSources(p, sourceDir)
		if err != nil {
			log.Fatalln("Load", p, "failed", err)
		}

		sources := baseSource.sources()
		for _, s := range sources {
			allSources[s.LocalPath] = s
		}
		var ss []Source
		for _, s := range allSources {
			ss = append(ss, s)
		}
		for _, s := range sources {
			if s.Path != "" {
				out[fileExt(s.LocalPath)[1:]]++
				if err := s.build(publicDir, ss); err != nil {
					log.Println("Build failed", s.LocalPath, err)
				}
			}
		}
		log.Println("Generated:")
		for k, v := range out {
			log.Println(k, ":", v)
		}
	}

	build("/")
	if serving {
		ss = newStaticServer(publicDir)
		watcher, err := fsnotify.NewWatcher()
		var mu sync.Mutex
		events := make(map[string]bool)
		if err != nil {
			log.Fatal(err)
		}

		defer watcher.Close()

		tplDir := filepath.Base(templateDir)
		dtDir := filepath.Base(dataDir)
		srcDir := filepath.SplitList(sourceDir)
		buildPath := func(path string) {
			time.Sleep(time.Millisecond * 500)
			ps := string(os.PathSeparator)
			p := strings.Split(path, ps)
			p = p[len(srcDir)-1:]
			if tplDir == p[0] || dtDir == p[0] {
				build("/")
			} else {
				build(strings.Join(p[1:], ps))
				log.Println("Rebuilt: ", path)
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

		err = watcher.Add(sourceDir)
		if err != nil {
			log.Fatal("Source DIR error: ", err)
		}
		err = watcher.Add(templateDir)
		if err != nil {
			log.Println("Template DIR error: ", err)
		}
		err = watcher.Add(dataDir)
		if err != nil {
			log.Println("Data DIR error: ", err)
		}
		for _, folder := range folders(sourceDir) {
			if err := watcher.Add(folder); err != nil {
				log.Println("Failed to watch dir: ", folder)
			}
		}

		log.Println("Serving: ", publicDir, " at ", fmt.Sprintf("http://localhost:%s", port))
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
		log.Fatal(err)
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
