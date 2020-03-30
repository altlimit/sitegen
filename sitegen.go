package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tdewolff/minify/v2"
	"gopkg.in/yaml.v2"
)

var (
	parseCtype = map[string]string{
		"text/css":       ".css",
		"application/js": ".js",
		"text/html":      ".html",
	}
)

type (
	// SiteGen is an instance of generator
	SiteGen struct {
		sitePath    string
		templateDir string
		dataDir     string
		publicDir   string
		sourceDir   string
		minify      *minify.M
		clean       bool
		sources     map[string]*Source
		dev         bool
	}
	// Source is a resource
	Source struct {
		Name  string
		Local string
		Path  string
		Meta  map[string]interface{}

		ext     string
		ctype   string
		content []byte
	}
)

func newSiteGen(sitePath, tplDir, dataDir, pubDir, sourceDir string, min *minify.M, clean bool, dev bool) *SiteGen {
	sp, err := filepath.Abs(sitePath)
	if err != nil {
		log.Fatal("Site Path ", sitePath, " error ", err)
	}
	sg := &SiteGen{
		sitePath:    sp,
		sourceDir:   sourceDir,
		templateDir: tplDir,
		dataDir:     dataDir,
		publicDir:   pubDir,
		minify:      min,
		clean:       clean,
		sources:     make(map[string]*Source),
		dev:         dev,
	}

	// load all sources keyed by local path
	filepath.Walk(filepath.Join(sg.sitePath, sg.sourceDir),
		func(path string, info os.FileInfo, err error) error {
			if info.IsDir() {
				return nil
			}
			if strings.HasPrefix(info.Name(), ".") {
				return nil
			}
			if err != nil {
				log.Println(path, " error ", err)
				return nil
			}
			s, err := sg.newSource(path)
			if err != nil {
				log.Println(path, " failed source ", err)
				return nil
			}
			// load it initially for meta data
			s.loadContent()
			sg.sources[path] = s
			return nil
		})

	return sg
}

func (sg *SiteGen) newSource(path string) (*Source, error) {
	s := &Source{
		Name: filepath.Base(path),
		ext:  fileExt(path),
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	s.Local = p
	s.Path = sg.localToPath(s)
	if ctype := mime.TypeByExtension(s.ext); ctype != "" {
		s.ctype = strings.Split(ctype, ";")[0]
	}
	return s, nil
}

func (sg *SiteGen) sourceList() []*Source {
	var sources []*Source
	for _, s := range sg.sources {
		sources = append(sources, s)
	}
	return sources
}

func (sg *SiteGen) html(s *Source) []byte {
	content := s.loadContent()
	if content == nil {
		return nil
	}
	tplName := filepath.Base(s.Local)
	if n, ok := s.Meta["template"]; ok {
		tplName = fmt.Sprint(n)
	}

	tpl := template.New(tplName)
	tpl = tpl.Funcs(map[string]interface{}{
		"sort":       sortBy,
		"limit":      limit,
		"offset":     offset,
		"getSources": sg.getSources,
		"data":       sg.data,
		"json":       parseJSON,
		"allowJS":    allowJS,
		"allowHTML":  allowHTML,
		"allowCSS":   allowCSS,
	})

	tplFiles, err := filepath.Glob(filepath.Join(sg.sitePath, sg.templateDir, "*.html"))
	if err != nil {
		log.Println("Load template ", s.Local, " error ", err)
		return nil
	}
	tpl, err = tpl.ParseFiles(tplFiles...)
	if err != nil {
		log.Println("Parse template ", s.Local, " error ", err)
		return nil
	}
	tpl, err = tpl.Parse(string(content))
	if err != nil {
		log.Println("Parse ", s.Local, " error ", err)
		return nil
	}
	data := map[string]interface{}{}
	for k, v := range s.Meta {
		data[k] = v
	}

	data["Dev"] = sg.dev
	data["Source"] = s

	tplBuf := new(bytes.Buffer)
	if err := tpl.Execute(tplBuf, data); err != nil {
		log.Println("Parse execute ", s.Local, " error ", err)
		return nil
	}
	body := tplBuf.Bytes()
	if sg.minify != nil {
		b, err := sg.minify.Bytes("text/html", body)
		if err != nil {
			log.Println("Minify ", s.Local, " error ", err)
		} else {
			body = b
		}
	}
	return body
}

func (sg *SiteGen) build(path string) error {
	s, ok := sg.sources[path]
	if !ok {
		return fmt.Errorf("Build failed for %s: not found", path)
	}

	switch s.ext {
	case ".html", ".htm":
		sDir := filepath.Join(sg.sitePath, sg.publicDir, s.Path)
		fName := "index.html"
		if strings.HasSuffix(s.Path, ".html") || strings.HasSuffix(s.Path, ".htm") {
			sDir, fName = filepath.Split(sDir)
		}
		if err := os.MkdirAll(sDir, os.ModePerm); err != nil {
			return err
		}

		pubPath := filepath.Join(sDir, fName)
		pubFile, err := os.Create(pubPath)
		if err != nil {
			return err
		}
		defer pubFile.Close()
		if body := sg.html(s); body != nil {
			_, err = pubFile.Write(body)
			if err != nil {
				return err
			}
		}
	default:
		src := s.loadContent()
		if src != nil {
			if serve, ok := s.Meta["serve"]; sg.dev && ok {
				go runCommand(fmt.Sprint(serve))
				return nil
			} else if build, ok := s.Meta["build"]; !sg.dev && ok {
				go runCommand(fmt.Sprint(build))
				return nil
			} else if sg.minify != nil && (s.ext == ".js" || s.ext == ".css") {
				if _, ok := parseCtype[s.ctype]; ok {
					b, err := sg.minify.Bytes(s.ctype, src)
					if err != nil {
						return err
					}
					src = b
				}
			}
		}
		if err := os.MkdirAll(filepath.Join(sg.sitePath, sg.publicDir, filepath.Dir(s.Path)), os.ModePerm); err != nil {
			return err
		}
		if err := ioutil.WriteFile(filepath.Join(sg.sitePath, sg.publicDir, s.Path), src, os.ModePerm); err != nil {
			return err
		}
	}

	return nil
}

func (sg *SiteGen) buildAll() {
	out := make(map[string]int)
	if sg.clean {
		if err := os.RemoveAll(filepath.Join(sg.sitePath, sg.publicDir)); err != nil {
			log.Fatalln("Failed to clean ", sg.publicDir, " error ", err)
		}
	}
	for k, s := range sg.sources {
		out[s.ext]++
		if err := sg.build(k); err != nil {
			log.Println("Build ", k, " error ", err)
		}
	}
	log.Println("Generated:")
	for k, v := range out {
		log.Println(k, ":", v)
	}
}

func (sg *SiteGen) data(name string) interface{} {
	path := filepath.Join(sg.sitePath, sg.dataDir, name)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Println("loadData failed", path, err)
		return nil
	}
	var d interface{}
	if err := json.Unmarshal(data, &d); err != nil {
		log.Println("loadData unmarshal failed", path, err)
		return nil
	}
	return d
}

func (sg *SiteGen) localToPath(s *Source) string {
	metaPath, ok := s.Meta["path"]
	var path string
	if ok {
		path = fmt.Sprint(metaPath)
	} else {
		path = strings.Replace(s.Local, filepath.Join(sg.sitePath, sg.sourceDir), "", 1)
		switch s.ext {
		case ".html", ".htm":
			path = strings.TrimSuffix(path, s.ext)
			if strings.HasSuffix(path, "index") {
				path = strings.TrimSuffix(path, "index")
			}
		}
		path = strings.ReplaceAll(path, "\\", "/")
	}

	return "/" + strings.TrimPrefix(path, "/")
}

func (sg *SiteGen) getSources(prop string, pattern string) []*Source {
	filtered := []*Source{}
	for _, s := range sg.sources {
		val := s.value(prop)
		match, err := filepath.Match(pattern, val)
		if err != nil {
			log.Println("Filter did not match", pattern, " = ", val)
			continue
		}
		if match {
			filtered = append(filtered, s)
		}
	}

	return filtered
}

func (s *Source) reloadContent() []byte {
	s.content = nil
	return s.loadContent()
}

func (s *Source) loadContent() []byte {
	if s.content == nil {
		var (
			meta    []byte
			content []byte
		)
		c, err := ioutil.ReadFile(s.Local)
		if err != nil {
			log.Println("Source loading failed ", err)
			return nil
		}
		_, txtCtype := parseCtype[s.ctype]
		if txtCtype {
			meta, content = parseContent(c, "---")
		} else {
			content = c
		}
		if txtCtype && meta != nil {
			if err := yaml.Unmarshal(meta, &s.Meta); err != nil {
				log.Println(s.Local, "meta error", err)
			} else {
				// override path
				if p, ok := s.Meta["path"]; ok {
					s.Path = fmt.Sprint(p)
				}
			}
		} else {
			s.Meta = make(map[string]interface{})
		}
		s.content = content
	}
	return s.content
}

func (s *Source) value(prop string) string {
	var val string
	switch prop {
	case "Path":
		val = s.Path
	case "Local":
		val = s.Local
	case "Filename":
		val = filepath.Base(s.Local)
	default:
		if strings.HasPrefix(prop, "Meta.") {
			val = fmt.Sprint(s.Meta[prop[5:]])
		}
	}
	return val
}

func parseContent(content []byte, sep string) ([]byte, []byte) {
	c := string(content)
	cc := c
	idx := strings.Index(c, sep)
	t := len(sep)
	if idx >= 0 {
		c = c[idx+t:]
		idx = strings.Index(c, sep)
		if idx >= 0 {
			c = c[:idx]
			return []byte(c), []byte(strings.ReplaceAll(cc, sep+c+sep, ""))
		}
	}
	return nil, content
}

func runCommand(run string) {
	cmdWG.Add(1)
	defer cmdWG.Done()
	c := strings.Split(run, " ")
	cmd := exec.Command(c[0], c[1:]...)
	stdout, err := cmd.Output()
	if err != nil {
		log.Println(err.Error())
		return
	}
	log.Println(string(stdout))
}

func fileExt(p string) string {
	return strings.ToLower(filepath.Ext(p))
}

func parseJSON(js interface{}) template.JS {
	var s string
	b, err := json.Marshal(js)
	if err != nil {
		log.Println("allowJS failed", js, err)
	} else {
		s = string(b)
	}

	return template.JS(s)
}

func allowJS(s string) template.JS {
	return template.JS(s)
}

func allowHTML(s string) template.HTML {
	return template.HTML(s)
}

func allowCSS(s string) template.CSS {
	return template.CSS(s)
}

func sortBy(prop string, order string, sources []*Source) []*Source {
	sorted := make([]*Source, len(sources))
	copy(sorted, sources)
	if order == "desc" {
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].value(prop) > sorted[j].value(prop)
		})
	} else {
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].value(prop) < sorted[j].value(prop)
		})
	}
	return sorted
}

func limit(limit int, sources []*Source) []*Source {
	if limit >= len(sources) {
		return sources
	}
	return sources[:limit]
}

func offset(offset int, sources []*Source) []*Source {
	if offset >= len(sources) {
		return []*Source{}
	}
	return sources[offset:]
}
