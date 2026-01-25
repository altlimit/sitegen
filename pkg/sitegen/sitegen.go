package sitegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
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
		SitePath    string
		TemplateDir string
		DataDir     string
		PublicPath  string
		BasePath    string
		SourceDir   string
		Minify      *minify.M
		Clean       bool
		Dev         bool

		sources    map[string]*Source
		genSources []*Source
		TplCache   map[string]*texttemplate.Template
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

func NewSiteGen(sitePath, tplDir, dataDir, sourceDir, pubPath, basePath string, min *minify.M, clean bool, dev bool) *SiteGen {
	sp, err := filepath.Abs(sitePath)
	if err != nil {
		log.Fatalln("Site Path ", sitePath, " error ", err)
	}
	sg := &SiteGen{
		SitePath:    sp,
		SourceDir:   sourceDir,
		TemplateDir: tplDir,
		DataDir:     dataDir,
		PublicPath:  pubPath,
		BasePath:    basePath,
		Minify:      min,
		Clean:       clean,
		sources:     make(map[string]*Source),
		TplCache:    make(map[string]*texttemplate.Template),
		Dev:         dev,
	}

	// load all sources keyed by local path
	filepath.Walk(filepath.Join(sg.SitePath, sg.SourceDir),
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
			_, err = sg.NewSource(path, false)
			if err != nil {
				log.Println(path, " failed source ", err)
				return nil
			}
			return nil
		})

	return sg
}

func (sg *SiteGen) NewSource(path string, gen bool) (*Source, error) {
	s := &Source{
		Name: filepath.Base(path),
		Ext:  fileExt(path),
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	s.Local = p
	if ctype := mime.TypeByExtension(s.Ext); ctype != "" {
		s.Ctype = strings.Split(ctype, ";")[0]
	}
	s.sg = sg
	s.LoadContent()
	if !gen {
		sg.sources[path] = s
	}
	return s, nil
}

func (sg *SiteGen) SourceList() []*Source {
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
		"path":     sg.Path,
		"sources":  sg.GetSources,
		"data":     sg.Data,
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
	content := s.LoadContent()
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
			sp, err = sg.NewSource(filepath.Join(sg.SitePath, sg.SourceDir, source), true)
			if err != nil {
				log.Println("page source error", err)
			}
			sp.Path += "/" + path
			sp.Name = path + sp.Ext
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
		if s.CurrentPage == 0 {
			s.TotalPages = int(math.Ceil(float64(rv.Len()) / float64(limit)))
			s.CurrentPage = 1
			if s.TotalPages > 1 {
				for i := 2; i <= s.TotalPages; i++ {
					sp := *s
					p := strconv.Itoa(i)
					sp.Path += "/" + p
					sp.Name = p + sp.Ext
					sp.CurrentPage = i
					sp.sg.genSources = append(sp.sg.genSources, &sp)
				}
			}
		}
		start := s.CurrentPage - 1
		start = start * limit
		end := start + limit
		if end > rv.Len() {
			end = rv.Len()
		}
		return rv.Slice(start, end).Interface()
	}

	var tpl *texttemplate.Template
	var err error

	// Try to get from cache
	if cached, ok := sg.TplCache[t]; ok {
		tpl, err = cached.Clone()
		if err != nil {
			log.Println("Template clone error", err)
			return nil
		}
		tpl.Funcs(funcs)
	} else {
		// Lazily load cache for this type
		if err := sg.LoadTemplate(t); err == nil {
			if cached, ok := sg.TplCache[t]; ok {
				tpl, err = cached.Clone()
				if err == nil {
					tpl.Funcs(funcs)
				}
			}
		} else {
			log.Println("LoadTemplate error", err)
		}
	}

	// Fallback to fresh parse if cache failed or not available (shouldn't happen if LoadTemplate works)
	if tpl == nil {
		tpl = texttemplate.New(tplName).Funcs(funcs)
		tplFiles, err := filepath.Glob(filepath.Join(sg.SitePath, sg.TemplateDir, "*."+t))
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
	}

	// Determine target template
	var target *texttemplate.Template
	if t := tpl.Lookup(tplName); t != nil {
		target = t
	} else {
		target = tpl
	}

	target, err = target.Parse(string(content))
	if err != nil {
		log.Println("Parse ", s.Local, " error ", err)
		return nil
	}

	data := map[string]interface{}{}
	for k, v := range s.Meta {
		data[k] = v
	}
	data["Path"] = s.path
	data["Page"] = s.CurrentPage
	data["Pages"] = s.TotalPages
	data["Dev"] = sg.Dev
	data["Source"] = s
	data["BasePath"] = sg.BasePath
	data["Today"] = time.Now().Format("2006-01-02")

	tplBuf := new(bytes.Buffer)
	if err := target.Execute(tplBuf, data); err != nil {
		log.Println("Parse execute ", s.Local, " error ", err)
		return nil
	}
	if t == "html" {
		body := tplBuf.Bytes()
		if sg.Minify != nil {
			b, err := sg.Minify.Bytes("text/html", body)
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
	switch s.Ext {
	case ".html", ".htm":
		sDir := filepath.Join(sg.PublicPath, s.Path)
		fName := "index.html"
		if strings.HasSuffix(s.Path, ".html") || strings.HasSuffix(s.Path, ".htm") {
			sDir, fName = filepath.Split(sDir)
		}
		return filepath.Join(sDir, fName)
	default:
		return filepath.Join(sg.PublicPath, s.Path)
	}
}

func (sg *SiteGen) Remove(path string) error {
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

func (sg *SiteGen) Build(path string) error {
	s, ok := sg.sources[path]
	if !ok {
		return fmt.Errorf("build failed for %s: not found", path)
	}

	pubPath := sg.sourcePath(s)
	src := s.LoadContent()

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
		switch s.Ext {
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
			if serve, ok := s.Meta["serve"]; sg.Dev && ok {
				runCommand(fmt.Sprint(serve))
				return nil
			} else if build, ok := s.Meta["build"]; !sg.Dev && ok {
				runCommand(fmt.Sprint(build))
				return nil
			} else if sg.Minify != nil && (s.Ext == ".js" || s.Ext == ".css") {
				if _, ok := parseCtype[s.Ctype]; ok {
					b, err := sg.Minify.Bytes(s.Ctype, src)
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
		if err := os.WriteFile(pubPath, src, os.ModePerm); err != nil {
			return err
		}
	}
	return nil
}

func (sg *SiteGen) BuildAll(reload bool) (map[string]int, error) {
	out := make(map[string]int)
	if sg.Clean {
		if err := os.RemoveAll(sg.PublicPath); err != nil {
			return nil, fmt.Errorf("failed to clean public path %s: %w", sg.PublicPath, err)
		}
	}
	sg.genSources = nil
	for k, s := range sg.sources {
		if reload {
			s.ReloadContent()
		}
		out[s.Ext]++
		if err := sg.Build(k); err != nil {
			log.Println("Build ", k, " error ", err)
		}
	}
	return out, nil
}

func (sg *SiteGen) ClearCache() {
	sg.TplCache = make(map[string]*texttemplate.Template)
}

func (sg *SiteGen) Path(path string) string {
	return sg.BasePath + strings.TrimLeft(path, "/")
}

func (sg *SiteGen) Data(name string) interface{} {
	path := filepath.Join(sg.SitePath, sg.DataDir, name)
	data, err := os.ReadFile(path)
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

func (sg *SiteGen) LocalToPath(s *Source) string {
	metaPath, ok := s.Meta["path"]
	var path string
	if ok {
		path = fmt.Sprint(metaPath)
	} else {
		path = strings.Replace(s.Local, filepath.Join(sg.SitePath, sg.SourceDir), "", 1)
		switch s.Ext {
		case ".html", ".htm":
			path = strings.TrimSuffix(path, s.Ext)
			path = strings.TrimSuffix(path, "index")
		}
		path = strings.ReplaceAll(path, "\\", "/")
	}
	return sg.BasePath + strings.TrimLeft(path, "/")
}

func (sg *SiteGen) GetSources(prop string, pattern string) []*Source {
	filtered := []*Source{}
	g, err := glob.Compile(pattern)
	if err != nil {
		log.Println("Pattern invalid ", pattern)
		return filtered
	}
	for _, s := range sg.sources {
		if g.Match(s.Value(prop)) {
			filtered = append(filtered, s)
		}
	}

	return filtered
}

// Helper Functions

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

func runCommand(run string) {
	c := strings.Split(run, " ")
	if len(c) == 0 {
		return
	}
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
		return src.Value(key)
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
	if s.TotalPages > 1 {
		for i := 1; i <= s.TotalPages; i++ {
			p := Page{
				Path:   s.Path,
				Page:   i,
				Active: i == s.CurrentPage,
			}
			if i > 1 {
				p.Path += "/" + strconv.Itoa(i)
			}
			pages = append(pages, p)
		}
	}
	return
}
