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
	parseExtensions = map[string]string{
		".css":  "text/css",
		".js":   "application/js",
		".htm":  "text/html",
		".html": "text/html",
	}
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
		Meta  map[string]string

		meta    []byte
		content []byte
		ext     string
		ctype   string
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
	c, err := ioutil.ReadFile(s.Local)
	if err != nil {
		return nil, err
	}
	if ctype := mime.TypeByExtension(s.ext); ctype != "" {
		s.ctype = strings.Split(ctype, ";")[0]
	}
	_, txtCtype := parseCtype[s.ctype]
	if txtCtype {
		s.meta, s.content = parseContent(c, "---")
	} else {
		s.meta = nil
		s.content = c
	}
	if txtCtype && s.meta != nil {
		if err := yaml.Unmarshal(c, &s.Meta); err != nil {
			log.Println(path, "meta error", err)
		} else {
			// override path
			if p, ok := s.Meta["path"]; ok {
				s.Path = p
			}
		}
	} else {
		s.Meta = make(map[string]string)
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
	tpl := template.New(s.Name)
	tpl = tpl.Funcs(map[string]interface{}{
		"sort":     sortBy,
		"limit":    limit,
		"offset":   offset,
		"filter":   filter,
		"data":     sg.data,
		"escapeJS": escapeJS,
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
	tpl, err = tpl.Parse(string(s.content))
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
	data["Sources"] = sg.sourceList()

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
		src := s.content
		if sg.dev {
			if serve, ok := s.Meta["serve"]; ok {
				go runCommand(serve)
				return nil
			} else if build, ok := s.Meta["build"]; ok {
				go runCommand(build)
				return nil
			} else if sg.minify != nil && (s.ext == ".js" || s.ext == ".css") {
				if _, ok := parseCtype[s.ctype]; ok {
					b, err := min.Bytes(s.ctype, src)
					if err != nil {
						return err
					}
					src = b
				}
			}
			if err := os.MkdirAll(filepath.Join(sg.sitePath, sg.publicDir, filepath.Dir(s.Path)), os.ModePerm); err != nil {
				return err
			}
			if err := ioutil.WriteFile(filepath.Join(sg.sitePath, sg.publicDir, s.Path), src, os.ModePerm); err != nil {
				return err
			}
		}
	}

	return nil
}

func (sg *SiteGen) buildAll() {
	for k := range sg.sources {
		if err := sg.build(k); err != nil {
			log.Println("Build ", k, " error ", err)
		}
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
	path := strings.Replace(s.Local, filepath.Join(sg.sitePath, sg.sourceDir), "", 1)
	switch s.ext {
	case ".html", ".htm":
		path = strings.TrimSuffix(path, s.ext)
		if strings.HasSuffix(path, "index") {
			path = strings.TrimSuffix(path, "index")
		}
	}
	path = strings.ReplaceAll(path, "\\", "/")
	return "/" + strings.TrimPrefix(path, "/")
}

func (s *Source) build(outputDir string, sources []Source) error {
	if s.Path == "" {
		return nil
	}

	src, err := ioutil.ReadFile(s.Local)
	if err != nil {
		return err
	}

	ext := fileExt(s.Local)
	switch ext {
	case ".html", ".htm":
		_, withPath := s.Meta["path"]
		sDir := filepath.Join(outputDir, s.Path)
		fName := "index.html"
		if withPath {
			sDir, fName = filepath.Split(sDir)
		}
		if err := os.MkdirAll(sDir, os.ModePerm); err != nil {
			return err
		}

		dstPath := filepath.Join(sDir, fName)
		dstFile, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		tpl, withTpl := s.Meta["template"]

		templates, err := filepath.Glob(filepath.Join(templateDir, "*.html"))
		if err != nil {
			return err
		}

		var tplPath string
		if withTpl {
			tplPath = filepath.Join(templateDir, tpl)
		} else {
			tplPath = s.Local
		}

		tmpl := template.New(filepath.Base(tplPath))
		tmpl = tmpl.Funcs(map[string]interface{}{
			"sort":     sortBy,
			"limit":    limit,
			"offset":   offset,
			"filter":   filter,
			"data":     loadData,
			"escapeJS": escapeJS,
		})

		tmpl, err = tmpl.ParseFiles(templates...)
		if err != nil {
			return err
		}

		_, src = parseContent(src, "---")
		tmpl, err = tmpl.Parse(string(src))
		if err != nil {
			return err
		}

		tplData := map[string]interface{}{}
		for k, v := range s.Meta {
			tplData[k] = v
		}
		tplData["Dev"] = serving
		tplData["Source"] = s
		tplData["Sources"] = sources
		tplBuf := new(bytes.Buffer)
		if err := tmpl.Execute(tplBuf, tplData); err != nil {
			return err
		}
		body := tplBuf.Bytes()
		if min != nil {
			b, err := min.Bytes("text/html", body)
			if err != nil {
				return err
			}
			body = b
		}
		_, err = dstFile.Write(body)
		if err != nil {
			return err
		}
	default:
		if _, ok := parseExtensions[ext]; ok {
			if c, _ := parseContent(src, "---"); c != nil {
				exec := make(map[string]interface{})
				if err := yaml.Unmarshal(c, &exec); err != nil {
					return err
				}
				if serving {
					if v, ok := exec["serve"]; ok {
						go runCommand(v.(string))
					}
				} else if v, ok := exec["build"]; ok {
					go runCommand(v.(string))
				}
				return nil
			} else if min != nil && (ext == ".js" || ext == ".css") {
				if ctype, ok := parseExtensions[ext]; ok {
					b, err := min.Bytes(ctype, src)
					if err != nil {
						return err
					}
					src = b
				}
			}
		}
		if err := os.MkdirAll(filepath.Join(outputDir, filepath.Dir(s.Path)), os.ModePerm); err != nil {
			return err
		}
		if err := ioutil.WriteFile(filepath.Join(outputDir, s.Path), src, os.ModePerm); err != nil {
			return err
		}
	}

	return nil
}

func (s *Source) build2(outputDir string, sources []Source) error {
	if s.Path == "" {
		return nil
	}

	src, err := ioutil.ReadFile(s.Local)
	if err != nil {
		return err
	}

	ext := fileExt(s.Local)
	switch ext {
	case ".html", ".htm":
		_, withPath := s.Meta["path"]
		sDir := filepath.Join(outputDir, s.Path)
		fName := "index.html"
		if withPath {
			sDir, fName = filepath.Split(sDir)
		}
		if err := os.MkdirAll(sDir, os.ModePerm); err != nil {
			return err
		}

		dstPath := filepath.Join(sDir, fName)
		dstFile, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		tpl, withTpl := s.Meta["template"]

		templates, err := filepath.Glob(filepath.Join(templateDir, "*.html"))
		if err != nil {
			return err
		}

		var tplPath string
		if withTpl {
			tplPath = filepath.Join(templateDir, tpl)
		} else {
			tplPath = s.Local
		}

		tmpl := template.New(filepath.Base(tplPath))
		tmpl = tmpl.Funcs(map[string]interface{}{
			"sort":     sortBy,
			"limit":    limit,
			"offset":   offset,
			"filter":   filter,
			"data":     loadData,
			"escapeJS": escapeJS,
		})

		tmpl, err = tmpl.ParseFiles(templates...)
		if err != nil {
			return err
		}

		_, src = parseContent(src, "---")
		tmpl, err = tmpl.Parse(string(src))
		if err != nil {
			return err
		}

		tplData := map[string]interface{}{}
		for k, v := range s.Meta {
			tplData[k] = v
		}
		tplData["Dev"] = serving
		tplData["Source"] = s
		tplData["Sources"] = sources
		tplBuf := new(bytes.Buffer)
		if err := tmpl.Execute(tplBuf, tplData); err != nil {
			return err
		}
		body := tplBuf.Bytes()
		if min != nil {
			b, err := min.Bytes("text/html", body)
			if err != nil {
				return err
			}
			body = b
		}
		_, err = dstFile.Write(body)
		if err != nil {
			return err
		}
	default:
		if _, ok := parseExtensions[ext]; ok {
			if c, _ := parseContent(src, "---"); c != nil {
				exec := make(map[string]interface{})
				if err := yaml.Unmarshal(c, &exec); err != nil {
					return err
				}
				if serving {
					if v, ok := exec["serve"]; ok {
						go runCommand(v.(string))
					}
				} else if v, ok := exec["build"]; ok {
					go runCommand(v.(string))
				}
				return nil
			} else if min != nil && (ext == ".js" || ext == ".css") {
				if ctype, ok := parseExtensions[ext]; ok {
					b, err := min.Bytes(ctype, src)
					if err != nil {
						return err
					}
					src = b
				}
			}
		}
		if err := os.MkdirAll(filepath.Join(outputDir, filepath.Dir(s.Path)), os.ModePerm); err != nil {
			return err
		}
		if err := ioutil.WriteFile(filepath.Join(outputDir, s.Path), src, os.ModePerm); err != nil {
			return err
		}
	}

	return nil
}

func (s Source) children() []Source {
	children := []Source{}
	// for _, c := range s.Children {
	// 	children = append(append(children, c), c.children()...)
	// }
	return children
}

func (s Source) sources() []Source {
	return append([]Source{s}, s.children()...)
}

func (s Source) value(prop string) string {
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

func loadSources(path, baseDir string) (Source, error) {
	fullPath := filepath.Join(baseDir, path)
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		return Source{}, err
	}
	if strings.HasPrefix(fileInfo.Name(), ".") {
		return Source{}, err
	}

	source := Source{}
	if fileInfo.IsDir() {
		fileInfos, err := ioutil.ReadDir(fullPath)
		if err != nil {
			return Source{}, err
		}

		for _, child := range fileInfos {
			if isIndex(child) {
				source.Path = path
				source.Local = filepath.Join(fullPath, child.Name())
			} else {
				childSource, err := loadSources(filepath.Join(path, child.Name()), baseDir)
				if err != nil {
					return source, err
				}
				if childSource.Local != "" {
					// source.Children = append(source.Children, childSource)
				}
			}
		}

	} else {
		source.Path = localToPath(path)
		source.Local = fullPath
	}

	if source.Local == "" {
		source.Local = fullPath
	} else {
		content, err := ioutil.ReadFile(source.Local)
		if err != nil {
			return source, err
		}
		if _, ok := parseExtensions[fileExt(source.Local)]; ok {
			if c, _ := parseContent(content, "---"); c != nil {
				if err := yaml.Unmarshal(c, &source.Meta); err != nil {
					return source, err
				}

				if p, ok := source.Meta["path"]; ok {
					source.Path = p
				}
			}
		}
	}

	return source, nil
}

func localToPath(path string) string {
	switch ext := fileExt(path); ext {
	case ".html", ".htm":
		path = strings.TrimSuffix(path, ext)
		if strings.HasSuffix(path, "index") {
			path = strings.TrimSuffix(path, "index")
		}
	}
	path = strings.ReplaceAll(path, "\\", "/")
	return "/" + strings.TrimPrefix(path, "/")
}

func isIndex(fileInfo os.FileInfo) bool {
	return strings.HasPrefix(fileInfo.Name(), "index.")
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

func filter(prop string, pattern string, sources []*Source) []*Source {
	filtered := []*Source{}
	for _, s := range sources {
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

func loadData(name string) interface{} {
	path := filepath.Join(dataDir, name)
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

func fileExt(p string) string {
	return strings.ToLower(filepath.Ext(p))
}

func escapeJS(js interface{}) template.JS {
	var s string
	b, err := json.Marshal(js)
	if err != nil {
		log.Println("escapeJS failed", js, err)
	} else {
		s = string(b)
	}

	return template.JS(s)
}
