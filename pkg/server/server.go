package server

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"sync"
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
			es.close();
			setTimeout(initHotReload, 2000);
		};
		window.addEventListener("beforeunload", function() {
			es.close();
		});
	}
}
initHotReload();
	</script>`

	// shareHotReloadScript is injected into pages served through the share
	// tunnel. The relay answers /__hotreload with a hibernatable WebSocket
	// rather than proxying SSE, so idle viewers cost nothing to keep open.
	shareHotReloadScript = `<script>
(function() {
	var delay = 2000;
	function connect() {
		if (typeof(WebSocket) === "undefined") return;
		var ws = new WebSocket((location.protocol === "https:" ? "wss://" : "ws://") + location.host + "/__hotreload");
		ws.onopen = function() { delay = 2000; };
		ws.onmessage = function(event) {
			if (event.data === "updated") location.reload();
		};
		ws.onclose = function() {
			setTimeout(connect, delay);
			delay = Math.min(delay * 2, 30000);
		};
	}
	connect();
})();
	</script>`
)

type shareCtxKey struct{}

// ShareHandler wraps ss for requests arriving through the public share
// tunnel. It hides dev-only endpoints (the CMS and the SSE live-reload
// stream, which the relay replaces with its own WebSocket endpoint) and
// injects the WebSocket live-reload script instead of the SSE one.
func ShareHandler(ss *StaticServer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/__cms") || r.URL.Path == "/__hotreload" {
			http.NotFound(w, r)
			return
		}
		ss.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), shareCtxKey{}, true)))
	})
}

func isShared(r *http.Request) bool {
	v, _ := r.Context().Value(shareCtxKey{}).(bool)
	return v
}

type StaticServer struct {
	PublicDir      string
	BaseDir        string
	Notifier       chan []byte
	newClients     chan chan []byte
	closingClients chan chan []byte
	clients        map[chan []byte]bool

	// CMS (dev-only) — set when the -cms flag is enabled.
	CMSEnabled bool
	SrcDir     string // absolute path to the site source directory
	DataDir    string // absolute path to the site data directory
	CMSAuth    string // "user:pass" for basic auth, or "" for none
}

func (ss *StaticServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if ss.CMSEnabled && strings.HasPrefix(r.URL.Path, "/__cms") {
		ss.serveCMS(w, r)
		return
	}
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
		fmt.Fprintf(w, "data: connected\n\n")
		flusher.Flush()

		messageChan := make(chan []byte, 1)
		ss.newClients <- messageChan
		defer func() {
			ss.closingClients <- messageChan
		}()
		for {
			select {
			case msg := <-messageChan:
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	} else {
		const indexPage = "/index.html"

		if strings.HasSuffix(r.URL.Path, indexPage) {
			localRedirect(w, r, "./")
			return
		}

		fs := http.Dir(ss.PublicDir)
		name := r.URL.Path
		f, err := fs.Open(name)
		if err != nil {
			// ignore favicon.ico
			if strings.HasSuffix(name, "favicon.ico") {
				return
			}
			if errM := err.Error(); strings.Contains(errM, "no such file") || strings.Contains(errM, "cannot find the file specified") {
				var err2 error
				f, err2 = fs.Open(ss.BaseDir + "404.html")
				if err2 == nil {
					err = nil
					log.Println(name, " not found in ", ss.PublicDir)
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
				script := hotReloadScript
				if isShared(r) {
					script = shareHotReloadScript
				}
				bb = strings.ReplaceAll(bb, "</body>", fmt.Sprintf("%s</body>", script))
				body = []byte(bb)
			}
			w.Write(body)
		}
	}
}

func (ss *StaticServer) Listen() {
	for {
		select {
		case s := <-ss.newClients:
			ss.clients[s] = true
		case s := <-ss.closingClients:
			delete(ss.clients, s)
		case event := <-ss.Notifier:
			for clientMessageChan := range ss.clients {
				select {
				case clientMessageChan <- event:
				default:
					// Slow client, skip message or it will block everyone
				}
			}
		}
	}
}

// Subscribe registers for rebuild notifications (the same events the SSE
// live-reload stream sends). Call the returned function to unsubscribe.
func (ss *StaticServer) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 1)
	ss.newClients <- ch
	var once sync.Once
	return ch, func() { once.Do(func() { ss.closingClients <- ch }) }
}

func NewStaticServer(dir, base string) *StaticServer {
	ss := &StaticServer{
		PublicDir:      dir,
		BaseDir:        base,
		Notifier:       make(chan []byte, 1),
		newClients:     make(chan chan []byte, 32),
		closingClients: make(chan chan []byte, 32),
		clients:        make(map[chan []byte]bool),
	}
	go ss.Listen()
	return ss
}

func localRedirect(w http.ResponseWriter, r *http.Request, newPath string) {
	if q := r.URL.RawQuery; q != "" {
		newPath += "?" + q
	}
	w.Header().Set("Location", newPath)
	w.WriteHeader(http.StatusMovedPermanently)
}
