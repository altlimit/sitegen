package main

import (
	"embed"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/altlimit/sitegen/pkg/server"
	"github.com/altlimit/sitegen/pkg/sitegen"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	version = "dev"

	// Styles
	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true).
			Padding(0, 1)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			PaddingLeft(1)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(1, 2)

	urlStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Underline(true)
)

//go:embed site/*
var siteFS embed.FS

// Messages
type buildMsg struct {
	stats map[string]int
	time  time.Time
}
type fileMsg struct {
	path   string
	action string
}
type statusMsg string

type errMsg string

type model struct {
	sg          *sitegen.SiteGen
	stats       map[string]int
	status      string
	serverURL   string
	publicPath  string
	srv         *server.StaticServer
	buildAll    bool
	tplDir      string
	exclude     string
	sourceDir   string
	lastBuild   time.Time
	recentFiles []string
	errorMsg    string
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}
	case buildMsg:
		if len(msg.stats) > 0 {
			m.stats = msg.stats
		}
		m.lastBuild = msg.time
		m.status = "Build complete"
		m.errorMsg = "" // Clear error on successful build
	case fileMsg:
		entry := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg.path)
		m.recentFiles = append(m.recentFiles, entry)
		if len(m.recentFiles) > 10 {
			m.recentFiles = m.recentFiles[1:]
		}
	case errMsg:
		m.errorMsg = string(msg)
		m.status = "Build failed"
	case statusMsg:
		m.status = string(msg)
	}
	return m, nil
}

func (m model) View() string {
	s := strings.Builder{}

	s.WriteString("\n" + headerStyle.Render(fmt.Sprintf("SiteGen %s", version)) + "\n\n")

	var statsView, infoView, activityView string

	if len(m.stats) > 0 {
		// Sort keys
		keys := make([]string, 0, len(m.stats))
		for k := range m.stats {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		rows := []string{}
		for _, k := range keys {
			rows = append(rows, fmt.Sprintf("%-10s %d", k, m.stats[k]))
		}

		statsView = boxStyle.Render(
			lipgloss.JoinVertical(lipgloss.Left,
				headerStyle.Render("Build Stats"),
				strings.Join(rows, "\n"),
			),
		)
	}

	if m.serverURL != "" {
		infoContent := fmt.Sprintf("Serving %s\n\n%s",
			lipgloss.NewStyle().Bold(true).Render(m.publicPath),
			urlStyle.Render(m.serverURL),
		)

		if !m.lastBuild.IsZero() {
			infoContent += fmt.Sprintf("\n\nLast Built: %s", m.lastBuild.Format("15:04:05"))
		}

		infoView = boxStyle.Render(
			lipgloss.JoinVertical(lipgloss.Left,
				headerStyle.Render("Server Info"),
				infoContent,
			),
		)
	}

	if len(m.recentFiles) > 0 {
		activityView = boxStyle.Render(
			lipgloss.JoinVertical(lipgloss.Left,
				headerStyle.Render("Recent Activity"),
				strings.Join(m.recentFiles, "\n"),
			),
		)
	}

	// Horizontal layout
	s.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, statsView, "  ", infoView, "  ", activityView) + "\n\n")

	if m.errorMsg != "" {
		s.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(m.errorMsg) + "\n\n")
	}

	if m.status != "" {
		s.WriteString(statusStyle.Render(m.status) + "\n")
	}

	s.WriteString(statusStyle.Render("\nPress q or Ctrl+C to quit"))

	return s.String()
}

func main() {
	var (
		sitePath    string
		publicPath  string
		sourceDir   string
		dataDir     string
		tplDir      string
		port        string
		basePath    string
		exclude     string
		create      bool
		serve       bool
		clean       bool
		isMinify    bool
		buildAll    bool
		showVersion bool
		min         *minify.M
		ss          *server.StaticServer
		sg          *sitegen.SiteGen
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
	flag.BoolVar(&buildAll, "buildall", false, "Always build all on change")
	flag.BoolVar(&showVersion, "version", false, "Show version")
	flag.StringVar(&port, "port", "8888", "Port for localhost")
	flag.Parse()

	if showVersion {
		fmt.Println(headerStyle.Render(fmt.Sprintf("SiteGen %s", version)))
		return
	}

	if create {
		if stat, err := os.Stat(sitePath); err == nil && stat.IsDir() {
			log.Fatalf("Directory %s exists", sitePath)
		}
		if err := copySite("site", sitePath); err != nil {
			log.Fatalf("Failed to create site: %v", err)
		}
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
	sg = sitegen.NewSiteGen(sitePath, tplDir, dataDir, sourceDir, pubPath, basePath, min, clean, serve)

	// Single run
	if !serve {
		fmt.Println(headerStyle.Render(fmt.Sprintf("SiteGen %s", version)))
		stats, err := sg.BuildAll(false)
		renderStats(stats)
		if err != nil {
			fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render("Build errors:"))
			fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(err.Error()))
			os.Exit(1)
		}
		return
	}

	// Serve mode
	portInt, _ := strconv.Atoi(port)
	finalPort := findNextAvailablePort(portInt)
	if finalPort != portInt {
		port = strconv.Itoa(finalPort)
	}

	ss = server.NewStaticServer(pubPath, basePath)
	serverURL := fmt.Sprintf("http://localhost:%s%s", port, basePath)

	m := model{
		sg:         sg,
		serverURL:  serverURL,
		publicPath: publicPath,
		srv:        ss,
		buildAll:   buildAll,
		tplDir:     tplDir,
		exclude:    exclude,
		sourceDir:  sourceDir,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())

	// Build initial
	go func() {
		stats, err := sg.BuildAll(false)
		if err != nil {
			p.Send(statusMsg(fmt.Sprintf("Build failed: %v", err)))
		} else {
			p.Send(buildMsg{stats: stats, time: time.Now()})
		}
	}()

	// Start watcher routine
	go runWatcher(p, sg, ss, exclude, sourceDir, tplDir, buildAll)

	// Start server
	go func() {
		http.Handle("/", ss)
		http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
	}()

	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}

func runWatcher(p *tea.Program, sg *sitegen.SiteGen, ss *server.StaticServer, exclude, sourceDir, tplDir string, buildAll bool) {
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
			p.Send(statusMsg(fmt.Sprintf("Failed to get absolute path %s error %v", path, err)))
		}
		time.Sleep(time.Millisecond * 500)
		fi, err := os.Stat(pp)
		if err != nil {
			// ignore stat errors (file deleted)
		}

		stats := map[string]int{}

		if err == nil && fi.IsDir() {
			if action == "add" {
				if !excluded(exclude, strings.Replace(pp, sg.SitePath+string(os.PathSeparator), "", 1)) {
					if err := watcher.Add(pp); err != nil {
						p.Send(statusMsg(fmt.Sprintf("Watch dir %s error %v", pp, err)))
					}
				}
			} else {
				if err := watcher.Remove(pp); err != nil {
					p.Send(statusMsg(fmt.Sprintf("Watch stop dir %s error %v", pp, err)))
				}
			}
		} else {
			rp := strings.Replace(pp, sg.SitePath, "", 1)
			isSrc := strings.HasPrefix(rp, string(os.PathSeparator)+sourceDir)
			switch action {
			case "add":
				if isSrc {
					p.Send(fileMsg{path: rp, action: "add"})
					if _, err := sg.NewSource(pp, false); err != nil {
						p.Send(statusMsg(fmt.Sprintf("%s failed source %v", path, err)))
					}

					if err := sg.Build(pp); err != nil {
						p.Send(errMsg(fmt.Sprintf("Build failed %s: %v", pp, err)))
					} else {
						// handled by fileMsg
					}

					if buildAll {
						s, err := sg.BuildAll(true)
						if err != nil {
							p.Send(errMsg(fmt.Sprintf("BuildAll failed: %v", err)))
						} else {
							stats = s
						}
					}
				} else {
					if strings.HasPrefix(rp, string(os.PathSeparator)+tplDir) {
						sg.ClearCache()
					}
					s, err := sg.BuildAll(true)
					if err != nil {
						p.Send(errMsg(fmt.Sprintf("BuildAll failed: %v", err)))
					} else {
						stats = s
					}
				}
			case "del":
				if isSrc {
					if err := sg.Remove(pp); err != nil {
						p.Send(statusMsg(fmt.Sprintf("Remove failed %s error %v", pp, err)))
					} else {
						p.Send(fileMsg{path: rp, action: "del"})
					}
					if buildAll {
						s, err := sg.BuildAll(true)
						if err != nil {
							p.Send(statusMsg(fmt.Sprintf("BuildAll failed %v", err)))
						} else {
							stats = s
						}
					}
				} else {
					if strings.HasPrefix(rp, string(os.PathSeparator)+tplDir) {
						sg.ClearCache()
					}
					s, err := sg.BuildAll(true)
					if err != nil {
						p.Send(statusMsg(fmt.Sprintf("BuildAll failed %v", err)))
					} else {
						stats = s
					}
				}
			}
		}
		mu.Lock()
		delete(events, key)
		mu.Unlock()
		cmdWG.Wait()
		ss.Notifier <- []byte("updated")

		// Send build message to update timestamp, even if stats are empty
		p.Send(buildMsg{stats: stats, time: time.Now()})
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

	if err = watcher.Add(sg.SitePath); err != nil {
		log.Fatalln("Watch dir ", tplDir, " error ", err)
	}

	filepath.Walk(sg.SitePath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				log.Fatal("watch dir", path, "error", err)
			}
			if info.IsDir() && !strings.HasPrefix(path, sg.PublicPath) {
				if !excluded(exclude, strings.Replace(path, sg.SitePath+string(os.PathSeparator), "", 1)) {
					err = watcher.Add(path)
					if err != nil {
						log.Fatal("watch dir", path, "error", err)
					}
					return nil
				}
			}
			return nil
		})

	// block forever
	select {}
}

func renderStats(stats map[string]int) {
	if len(stats) == 0 {
		return
	}

	// Sort keys
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	rows := []string{}
	for _, k := range keys {
		rows = append(rows, fmt.Sprintf("%-10s %d", k, stats[k]))
	}

	fmt.Println(boxStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			headerStyle.Render("Build Stats"),
			strings.Join(rows, "\n"),
		),
	))
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

func copySite(folder, target string) error {
	entries, err := siteFS.ReadDir(folder)
	if err != nil {
		return fmt.Errorf("copySite %s ReadDir error %v", folder, err)
	}
	for _, entry := range entries {
		p := filepath.Join(folder, entry.Name())
		if entry.IsDir() {
			if err := copySite(p, target); err != nil {
				return err
			}
		} else {
			b, err := siteFS.ReadFile(p)
			if err != nil {
				return fmt.Errorf("copySite ReadFile %s error %v", p, err)
			}
			to := filepath.Join(target, p[strings.Index(p, "/"):])
			os.MkdirAll(filepath.Dir(to), os.ModePerm)
			if err := os.WriteFile(to, b, 0644); err != nil {
				return fmt.Errorf("copySite WriteFile %s error %v", p, err)
			}
		}
	}
	return nil
}

func findNextAvailablePort(startPort int) int {
	for port := startPort; port < startPort+100; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			ln.Close()
			return port
		}
	}
	return startPort // Fallback to original if we can't find one in range
}
