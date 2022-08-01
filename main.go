package main

import (
	"embed"
	_ "embed"
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
	version = "v0.0.11"
)

//go:embed site/*
var siteFS embed.FS

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
		exclude    string
		create     bool
		serve      bool
		clean      bool
		isMinify   bool
		min        *minify.M
		ss         *staticServer
		sg         *SiteGen
	)
	flag.BoolVar(&create, "create", false, "Creates a new site template")
	flag.StringVar(&sitePath, "site", "./site", "Absolute or relative root site path")
	flag.StringVar(&sourceDir, "source", "src", "Source folder relative to site path")
	flag.StringVar(&dataDir, "data", "data", "Data folder relative to site path")
	flag.StringVar(&tplDir, "templates", "templates", "Template folder relative to site path")
	flag.StringVar(&publicPath, "public", "./public", "Absolute or relative public path")
	flag.StringVar(&basePath, "base", "/", "Base folder relative to public path")
	flag.BoolVar(&serve, "serve", false, "Start a development server and watcher")
	flag.StringVar(&exclude, "exclude", "^(node_modules|bower_components)", "Exclude from watcher")
	flag.BoolVar(&clean, "clean", false, "Clean public dir before build")
	flag.BoolVar(&isMinify, "minify", false, "Minify (HTML|JS|CSS)")
	flag.StringVar(&port, "port", "8888", "Port for localhost")
	flag.Parse()

	if create {
		copySite("site", sitePath)
		log.Println("Site template created: ", sitePath)
		return
	}

	if isMinify {
		min = minify.New()
		min.AddFunc("text/css", css.Minify)
		min.AddFunc("text/xml", xml.Minify)
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

		processKey := func(key string) {
			action := key[:3]
			path := key[4:]
			pp, err := filepath.Abs(path)
			if err != nil {
				log.Println("Failed to get absolute path ", path, " error ", err)
			}
			time.Sleep(time.Millisecond * 500)
			fi, err := os.Stat(pp)
			if err != nil {
				log.Println("Stat error ", err)
			}
			if err == nil && fi.IsDir() {
				if action == "add" {
					if !excluded(exclude, strings.Replace(pp, sg.sitePath+string(os.PathSeparator), "", 1)) {
						if err := watcher.Add(pp); err != nil {
							log.Println("Watch dir ", pp, " error ", err)
						}
					}
				} else {
					if err := watcher.Remove(pp); err != nil {
						log.Println("Watch stop dir ", pp, " error ", err)
					}
				}
			} else {
				rp := strings.Replace(pp, sg.sitePath, "", 1)
				isSrc := strings.HasPrefix(rp, string(os.PathSeparator)+sourceDir)
				switch action {
				case "add":
					if isSrc {
						if s, ok := sg.sources[pp]; ok {
							s.reloadContent()
						} else {
							if _, err := sg.newSource(pp); err != nil {
								log.Println(path, " failed source ", err)
							}
						}
						if err := sg.build(pp); err != nil {
							log.Println("Build failed ", pp, " error ", err)
						} else {
							log.Println("Rebuilt: ", rp)
						}
					} else {
						sg.buildAll()
					}
				case "del":
					if isSrc {
						if err := sg.remove(pp); err != nil {
							log.Println("Remove failed ", pp, " error ", err)
						} else {
							log.Println("Deleted: ", rp)
						}
					} else {
						sg.buildAll()
					}
				}
			}
			mu.Lock()
			delete(events, key)
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
					var op string
					if event.Op&fsnotify.Remove == fsnotify.Remove || event.Op&fsnotify.Rename == fsnotify.Rename {
						op = "del"
					} else if event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Write == fsnotify.Write {
						op = "add"
					}
					if op != "" {
						if n := strings.Split(event.Name, string(os.PathSeparator)); strings.HasPrefix(n[len(n)-1], ".") {
							return
						}
						b := false
						mu.Lock()
						key := op + ":" + event.Name
						if _, ok := events[key]; !ok {
							events[key] = true
							b = true
						}
						mu.Unlock()
						if b {
							go processKey(key)
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

		if err = watcher.Add(sg.sitePath); err != nil {
			log.Fatalln("Watch dir ", tplDir, " error ", err)
		}

		filepath.Walk(sg.sitePath,
			func(path string, info os.FileInfo, err error) error {
				if err != nil {
					log.Fatal("watch dir", path, "error", err)
				}
				if info.IsDir() && !strings.HasPrefix(path, sg.publicPath) {
					if !excluded(exclude, strings.Replace(path, sg.sitePath+string(os.PathSeparator), "", 1)) {
						err = watcher.Add(path)
						if err != nil {
							log.Fatal("watch dir", path, "error", err)
						}
						return nil
					}
				}
				return nil
			})

		log.Println("Serving: ", publicPath, " at ", fmt.Sprintf("http://localhost:%s%s", port, basePath))
		log.Println("Press Ctrl+C to stop")
		http.Handle("/", ss)
		log.Println(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
	}

	cmdWG.Wait()
}

func excluded(pattern, path string) bool {
	if strings.HasPrefix(path, ".") {
		return true
	}
	m, err := regexp.Match(pattern, []byte(path))
	if err != nil {
		log.Println("Watch exclude pattern", pattern, " error ", err)
		return true
	}
	return m
}

func copySite(folder, target string) {
	entries, err := siteFS.ReadDir(folder)
	if err != nil {
		log.Fatalf("copySite %s ReadDir error %v", folder, err)
	}
	for _, entry := range entries {
		p := filepath.Join(folder, entry.Name())
		if entry.IsDir() {
			copySite(p, target)
		} else {
			b, err := siteFS.ReadFile(p)
			if err != nil {
				log.Fatalf("copySite ReadFile %s error %v", p, err)
			}
			to := filepath.Join(target, p[strings.Index(p, "/"):])
			os.MkdirAll(filepath.Dir(to), os.ModePerm)
			if err := ioutil.WriteFile(to, b, 0644); err != nil {
				log.Fatalf("copySite WriteFile %s error %v", p, err)
			}
		}
	}
}
