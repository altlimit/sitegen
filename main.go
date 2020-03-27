package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

var (
	templateDir string
)

func main() {
	var (
		publicDir string
		sourceDir string
		serve     bool
		port      string
		ss        *staticServer
	)
	flag.StringVar(&publicDir, "public", "./public", "Public directory")
	flag.StringVar(&sourceDir, "source", "./src", "Source directory")
	flag.StringVar(&templateDir, "templates", "./templates", "Template directory")
	flag.BoolVar(&serve, "serve", os.Getenv("SERVE") == "1", "Watch for changes & serve")
	flag.StringVar(&port, "port", "8888", "Port for localhost")
	flag.Parse()

	if err := os.RemoveAll(publicDir); err != nil {
		log.Fatalln(err)
	}

	allSources := make(map[string]Source)

	build := func(p string) {
		out := make(map[string]int)
		baseSource, err := loadSources(p, sourceDir)
		if err != nil {
			log.Fatalln(err)
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
				out[s.ext()[1:]]++
				if err := s.build(publicDir, ss); err != nil {
					log.Fatalln(err)
				}
			}
		}
		log.Println("Generated:")
		for k, v := range out {
			log.Println(k, ":", v)
		}
	}

	build("/")
	if serve {
		ss = newStaticServer(publicDir)
		watcher, err := fsnotify.NewWatcher()
		var mu sync.Mutex
		events := make(map[string]bool)
		if err != nil {
			log.Fatal(err)
		}

		defer watcher.Close()

		tplDir := filepath.Base(templateDir)
		srcDir := filepath.SplitList(sourceDir)
		buildPath := func(path string) {
			time.Sleep(time.Millisecond * 500)
			ps := string(os.PathSeparator)
			p := strings.Split(path, ps)
			p = p[len(srcDir)-1:]
			if tplDir == p[0] {
				build("/")
			} else {
				build(strings.Join(p[1:], ps))
				log.Println("Rebuilt: ", path)
			}
			mu.Lock()
			delete(events, path)
			mu.Unlock()
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
