package main

import (
	"bytes"
	"fmt"
	"log"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
)

var (
	hotReloadScript = `<script>
function initHotReload() {
	if (typeof(EventSource) !== "undefined") {
		const es = new EventSource("/__hotreload");
		es.onmessage = function(event) {
			if (event.data === "updated") {
				location.reload();
			}
		}
		es.onerror = function(err) {
			console.error("ES:", err);
			es.close();
			setTimeout(initHotReload, 5000);
		};
	}
}
initHotReload();
	</script>`
)

type staticServer struct {
	publicDir      string
	baseDir        string
	notifier       chan []byte
	newClients     chan chan []byte
	closingClients chan chan []byte
	clients        map[chan []byte]bool
}

func (ss *staticServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/__hotreload" {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		messageChan := make(chan []byte)
		ss.newClients <- messageChan
		defer func() {
			ss.closingClients <- messageChan
		}()

		notify := r.Context().Done()

		go func() {
			<-notify
			ss.closingClients <- messageChan
		}()

		for {
			fmt.Fprintf(w, "data: %s\n\n", <-messageChan)
			flusher.Flush()
		}
	} else {
		const indexPage = "/index.html"

		if strings.HasSuffix(r.URL.Path, indexPage) {
			localRedirect(w, r, "./")
			return
		}

		fs := http.Dir(ss.publicDir)
		name := r.URL.Path
		f, err := fs.Open(name)
		if err != nil {
			// ignore favicon.ico
			if strings.HasSuffix(name, "favicon.ico") {
				return
			}
			if errM := err.Error(); strings.Contains(errM, "no such file") || strings.Contains(errM, "cannot find the file specified") {
				var err2 error
				f, err2 = fs.Open(ss.baseDir + "404.html")
				if err2 == nil {
					err = nil
					log.Println(name, " not found in ", ss.publicDir)
				}
			}
			if err != nil {
				log.Println(name, " error ", err)
				return
			}
		}
		defer f.Close()

		d, err := f.Stat()
		if err != nil {
			log.Println(r.URL.Path, " stat error ", err)
			return
		}

		if d.IsDir() {
			url := r.URL.Path
			if url[len(url)-1] != '/' {
				localRedirect(w, r, path.Base(url)+"/")
				return
			}
		}

		// use contents of index.html for directory, if present
		if d.IsDir() {
			index := strings.TrimSuffix(name, "/") + indexPage
			ff, err := fs.Open(index)
			if err == nil {
				defer ff.Close()
				dd, err := ff.Stat()
				if err == nil {
					d = dd
					f = ff
				}
			}
		}

		if ctype := mime.TypeByExtension(filepath.Ext(d.Name())); ctype != "" {
			w.Header().Set("Content-Type", ctype)
		}
		w.WriteHeader(http.StatusOK)

		if r.Method != "HEAD" {
			buf := new(bytes.Buffer)
			buf.ReadFrom(f)
			body := buf.Bytes()
			if err != nil {
				log.Println("Error writing response: ", err)
			}
			if bb := string(body); strings.Contains(bb, "</body>") {
				bb = strings.ReplaceAll(bb, "</body>", fmt.Sprintf("%s</body>", hotReloadScript))
				body = []byte(bb)
			}
			w.Write(body)
		}
	}
}

func (ss *staticServer) listen() {
	for {
		select {
		case s := <-ss.newClients:
			ss.clients[s] = true
		case s := <-ss.closingClients:
			delete(ss.clients, s)
		case event := <-ss.notifier:
			for clientMessageChan := range ss.clients {
				clientMessageChan <- event
			}
		}
	}

}

func newStaticServer(dir, base string) *staticServer {
	ss := &staticServer{
		publicDir:      dir,
		baseDir:        base,
		notifier:       make(chan []byte, 1),
		newClients:     make(chan chan []byte),
		closingClients: make(chan chan []byte),
		clients:        make(map[chan []byte]bool),
	}
	go ss.listen()
	return ss
}

func localRedirect(w http.ResponseWriter, r *http.Request, newPath string) {
	if q := r.URL.RawQuery; q != "" {
		newPath += "?" + q
	}
	w.Header().Set("Location", newPath)
	w.WriteHeader(http.StatusMovedPermanently)
}
