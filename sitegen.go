package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"math"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/gobwas/glob"
	"github.com/tdewolff/minify/v2"
	"gopkg.in/yaml.v2"
)

var (
	parseCtype = map[string]string{
		"text/css":               ".css",
		"application/javascript": ".js",
		"text/html":              ".html",
		"text/xml":               ".xml",
		"application/xml":        ".xml",
		"text/plain":             ".txt",
		"text/markdown":          ".md",
	}
)

type (
	// SiteGen is an instance of generator
	SiteGen struct {
		sitePath    string
		templateDir string
		dataDir     string
		publicPath  string
		basePath    string
		sourceDir   string
		minify      *minify.M
		clean       bool
		sources     map[string]*Source
		genSources  []*Source
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
		sg      *SiteGen
		page    int
		pages   int
		path    string
	}

	Parser func(*Source) []byte
	Page   struct {
		Active bool
		Path   string
		Page   int
	}

	kv struct {
		Key   string
		Value interface{}
	}
)

func newSiteGen(sitePath, tplDir, dataDir, sourceDir, pubPath, basePath string, min *minify.M, clean bool, dev bool) *SiteGen {
	sp, err := filepath.Abs(sitePath)
	if err != nil {
		log.Fatalln("Site Path ", sitePath, " error ", err)
	}
	sg := &SiteGen{
		sitePath:    sp,
		sourceDir:   sourceDir,
		templateDir: tplDir,
		dataDir:     dataDir,
		publicPath:  pubPath,
		basePath:    basePath,
		minify:      min,
		clean:       clean,
		sources:     make(map[string]*Source),
		dev:         dev,
	}

	// load all sources keyed by local path
	filepath.Walk(filepath.Join(sg.sitePath, sg.sourceDir),
		func(path string, info os.FileInfo, err error) error {
			if info == nil || info.IsDir() {
				return nil
			}
			if strings.HasPrefix(info.Name(), ".") {
				return nil
			}
			if err != nil {
				log.Println(path, " error ", err)
				return nil
			}
			_, err = sg.newSource(path, false)
			if err != nil {
				log.Println(path, " failed source ", err)
				return nil
			}
			return nil
		})

	return sg
}

func (sg *SiteGen) newSource(path string, gen bool) (*Source, error) {
	s := &Source{
		Name: filepath.Base(path),
		ext:  fileExt(path),
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	s.Local = p
	if ctype := mime.TypeByExtension(s.ext); ctype != "" {
		s.ctype = strings.Split(ctype, ";")[0]
	}
	s.sg = sg
	s.loadContent()
	if !gen {
		sg.sources[path] = s
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

func (sg *SiteGen) tplFuncs() map[string]interface{} {
	return map[string]interface{}{
		"sort":     sortBy,
		"limit":    limit,
		"offset":   offset,
		"path":     sg.path,
		"sources":  sg.getSources,
		"data":     sg.data,
		"json":     parseJSON,
		"js":       allowJS,
		"html":     allowHTML,
		"css":      allowCSS,
		"contains": contains,
		"pages":    pages,
		"select":   mapToList,
		"filter":   filterBy,
	}
}

func (sg SiteGen) parse(s *Source, t string) []byte {
	content := s.loadContent()
	if content == nil {
		return nil
	}
	tplName := filepath.Base(s.Local)
	if n, ok := s.Meta["template"]; ok {
		tplName = fmt.Sprint(n)
	}

	funcs := sg.tplFuncs()
	funcs["page"] = func(source, path string) string {
		var sp *Source
		for i := range sg.genSources {
			if sg.genSources[i].path == path {
				sp = sg.genSources[i]
				break
			}
		}
		if sp == nil {
			var err error
			sp, err = sg.newSource(filepath.Join(sg.sitePath, sg.sourceDir, source), true)
			if err != nil {
				log.Println("page source error", err)
			}
			sp.Path += "/" + path
			sp.Name = path + sp.ext
			sp.path = path
			s.sg.genSources = append(s.sg.genSources, sp)
		}
		return sp.Path
	}
	funcs["paginate"] = func(limit int, list interface{}) interface{} {
		rv := reflect.ValueOf(list)
		if rv.Kind() != reflect.Slice {
			log.Println("paginate must be of type Slice got " + rv.Type().String())
		}
		if s.page == 0 {
			s.pages = int(math.Ceil(float64(rv.Len()) / float64(limit)))
			s.page = 1
			if s.pages > 1 {
				for i := 2; i <= s.pages; i++ {
					sp := *s
					p := strconv.Itoa(i)
					sp.Path += "/" + p
					sp.Name = p + sp.ext
					sp.page = i
					sp.sg.genSources = append(sp.sg.genSources, &sp)
				}
			}
		}
		start := s.page - 1
		start = start * limit
		end := start + limit
		if end > rv.Len() {
			end = rv.Len()
		}
		return rv.Slice(start, end).Interface()
	}

	tpl := texttemplate.New(tplName)
	tpl = tpl.Funcs(funcs)

	tplFiles, err := filepath.Glob(filepath.Join(sg.sitePath, sg.templateDir, "*."+t))
	if err != nil {
		log.Println("Load template ", s.Local, " error ", err)
		return nil
	}
	if len(tplFiles) > 0 {
		tpl, err = tpl.ParseFiles(tplFiles...)
		if err != nil {
			log.Println("Parse template ", s.Local, " error ", err)
			return nil
		}
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
	data["Path"] = s.path
	data["Page"] = s.page
	data["Pages"] = s.pages
	data["Dev"] = sg.dev
	data["Source"] = s
	data["BasePath"] = sg.basePath
	data["Today"] = time.Now().Format("2006-01-02")

	tplBuf := new(bytes.Buffer)
	if err := tpl.Execute(tplBuf, data); err != nil {
		log.Println("Parse execute ", s.Local, " error ", err)
		return nil
	}
	if t == "html" {
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
	return tplBuf.Bytes()
}

func (sg *SiteGen) text(s *Source) []byte {
	return sg.parse(s, "txt")
}

func (sg *SiteGen) html(s *Source) []byte {
	return sg.parse(s, "html")
}

func (sg *SiteGen) sourcePath(s *Source) string {
	switch s.ext {
	case ".html", ".htm":
		sDir := filepath.Join(sg.publicPath, s.Path)
		fName := "index.html"
		if strings.HasSuffix(s.Path, ".html") || strings.HasSuffix(s.Path, ".htm") {
			sDir, fName = filepath.Split(sDir)
		}
		return filepath.Join(sDir, fName)
	default:
		return filepath.Join(sg.publicPath, s.Path)
	}
}

func (sg *SiteGen) remove(path string) error {
	s, ok := sg.sources[path]
	if !ok {
		return nil
	}

	pubPath := sg.sourcePath(s)
	if err := os.Remove(pubPath); err != nil {
		return fmt.Errorf("remove failed for %s: error %v", pubPath, err)
	}
	pubPath = filepath.Dir(pubPath)
	empty, err := isDirEmpty(pubPath)
	if err != nil {
		return fmt.Errorf("remove dir check for %s: error %v", pubPath, err)
	}
	if empty {
		if err := os.Remove(pubPath); err != nil {
			return fmt.Errorf("remove dir failed for %s: error %v", pubPath, err)
		}
	}

	return nil
}

func (sg *SiteGen) build(path string) error {
	s, ok := sg.sources[path]
	if !ok {
		return fmt.Errorf("build failed for %s: not found", path)
	}

	pubPath := sg.sourcePath(s)
	src := s.loadContent()

	// check if parametarized page then skip if no parameter
	if strings.Contains(string(src), " .Path") && s.path == "" {
		return nil
	}

	var parser Parser
	// force parse template any file if --- parse: text --- is found
	if p, ok := s.Meta["parse"].(string); ok {
		switch p {
		case "text":
			parser = sg.text
		case "html":
			parser = sg.html
		}
	} else {
		switch s.ext {
		case ".txt":
			parser = sg.text
		case ".html", ".htm":
			parser = sg.html
		}
	}
	if parser != nil {
		if err := os.MkdirAll(filepath.Dir(pubPath), os.ModePerm); err != nil {
			return err
		}
		pubFile, err := os.Create(pubPath)
		if err != nil {
			return err
		}
		defer pubFile.Close()

		if body := parser(s); body != nil {
			_, err = pubFile.Write(body)
			if err != nil {
				return err
			}
		}
		for {
			if len(sg.genSources) == 0 {
				break
			}
			cs := sg.genSources[0]
			sg.genSources = sg.genSources[1:]
			childPath := sg.sourcePath(cs)
			if err := os.MkdirAll(filepath.Dir(childPath), os.ModePerm); err != nil {
				return err
			}
			childFile, err := os.Create(childPath)
			if err != nil {
				return err
			}
			_, err = childFile.Write(parser(cs))
			if err != nil {
				childFile.Close()
				return err
			}
			childFile.Close()
		}
	} else {
		if src != nil {
			if serve, ok := s.Meta["serve"]; sg.dev && ok {
				runCommand(fmt.Sprint(serve))
				return nil
			} else if build, ok := s.Meta["build"]; !sg.dev && ok {
				runCommand(fmt.Sprint(build))
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
		if err := os.MkdirAll(filepath.Dir(pubPath), os.ModePerm); err != nil {
			return err
		}
		if err := ioutil.WriteFile(pubPath, src, os.ModePerm); err != nil {
			return err
		}
	}
	return nil
}

func (sg *SiteGen) buildAll(reload bool) {
	out := make(map[string]int)
	if sg.clean {
		if err := os.RemoveAll(sg.publicPath); err != nil {
			log.Fatalln("Failed to clean ", sg.publicPath, " error ", err)
		}
	}
	sg.genSources = nil
	for k, s := range sg.sources {
		if reload {
			s.reloadContent()
		}
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

func (sg *SiteGen) path(path string) string {
	return sg.basePath + strings.TrimLeft(path, "/")
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
			path = strings.TrimSuffix(path, "index")
		}
		path = strings.ReplaceAll(path, "\\", "/")
	}
	return sg.basePath + strings.TrimLeft(path, "/")
}

func (sg *SiteGen) getSources(prop string, pattern string) []*Source {
	filtered := []*Source{}
	g, err := glob.Compile(pattern)
	if err != nil {
		log.Println("Pattern invalid ", pattern)
		return filtered
	}
	for _, s := range sg.sources {
		if g.Match(s.value(prop)) {
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
		s.page = 0
		s.pages = 0
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
		s.Meta = make(map[string]interface{})
		if txtCtype && meta != nil {
			if err := yaml.Unmarshal(meta, &s.Meta); err != nil {
				log.Println(s.Local, "meta error", err)
			} else {
				// override path
				if p, ok := s.Meta["path"]; ok {
					s.Path = fmt.Sprint(p)
				}
			}
		}
		s.content = content
	}
	s.Path = s.sg.localToPath(s)
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
	out := string(stdout)
	if out == "" {
		out = run
	}
	log.Println(strings.Trim(out, "\n"))
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

func contains(sub, s string) bool {
	return strings.Contains(s, sub)
}

func sortBy(prop string, order string, list interface{}) (result []interface{}) {
	rv := reflect.ValueOf(list)
	if rv.Kind() != reflect.Slice {
		log.Println("sort must be of type Slice got " + rv.Type().String())
	}

	sorted := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
	reflect.Copy(sorted, rv)
	sort.Slice(sorted.Interface(), func(i, j int) bool {
		if order == "desc" {
			return valueOf(prop, sorted.Index(i)) > valueOf(prop, sorted.Index(j))
		}
		return valueOf(prop, sorted.Index(i)) < valueOf(prop, sorted.Index(j))
	})

	for i := 0; i < sorted.Len(); i++ {
		result = append(result, sorted.Index(i).Interface())
	}
	return
}

func valueOf(key string, a reflect.Value) string {
	v := a.Interface()
	if src, ok := v.(*Source); ok {
		return src.value(key)
	}
	if val, ok := v.(kv); ok {
		if key == "Key" {
			return val.Key
		} else if key == "Value" {
			if v, ok := val.Value.(string); ok {
				return v
			}
		} else if strings.HasPrefix(key, "Value.") {
			key = strings.Split(key, ".")[1]
			v = val.Value
		}

	}
	if val, ok := v.(map[string]interface{}); ok {
		if v, ok := val[key]; ok {
			if vv, ok := v.(string); ok {
				return vv
			}
		}
	}
	return ""
}

func filterBy(prop string, pattern string, list interface{}) (result []interface{}) {
	rv := reflect.ValueOf(list)
	if rv.Kind() != reflect.Slice {
		log.Println("sort must be of type Slice got " + rv.Type().String())
	}

	for i := 0; i < rv.Len(); i++ {
		v := rv.Index(i)
		val := valueOf(prop, v)
		ok, err := regexp.Match(pattern, []byte(val))
		if err != nil {
			log.Println("filter match error", err)
		}
		if ok {
			result = append(result, v.Interface())
		}
	}
	return
}

func mapToList(d map[string]interface{}) (result []kv) {
	for k, v := range d {
		result = append(result, kv{Key: k, Value: v})
	}
	return
}

func limit(limit int, list interface{}) interface{} {
	rv := reflect.ValueOf(list)
	if rv.Kind() != reflect.Slice {
		log.Println("sort must be of type Slice got " + rv.Type().String())
	}

	if limit >= rv.Len() {
		return list
	}
	return rv.Slice(0, limit).Interface()
}

func offset(offset int, list interface{}) interface{} {
	rv := reflect.ValueOf(list)
	if rv.Kind() != reflect.Slice {
		log.Println("sort must be of type Slice got " + rv.Type().String())
	}

	if offset >= rv.Len() {
		return reflect.MakeSlice(rv.Type(), 0, 0).Interface()
	}
	return rv.Slice(offset, rv.Len()).Interface()
}

func pages(s *Source) (pages []Page) {
	if s.pages > 1 {
		for i := 1; i <= s.pages; i++ {
			p := Page{
				Path:   s.Path,
				Page:   i,
				Active: i == s.page,
			}
			if i > 1 {
				p.Path += "/" + strconv.Itoa(i)
			}
			pages = append(pages, p)
		}
	}
	return
}

func isDirEmpty(name string) (bool, error) {
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}
	return false, err
}
