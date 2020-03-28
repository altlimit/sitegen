package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v2"
)

var (
	parseExtensions = map[string]bool{
		".css":  true,
		".js":   true,
		".htm":  true,
		".html": true,
	}
)

// Source represents a resource
type Source struct {
	Children  []Source
	LocalPath string
	Path      string
	Meta      map[string]interface{}
}

func (s *Source) build(outputDir string, sources []Source) error {
	if s.Path == "" {
		return nil
	}

	src, err := ioutil.ReadFile(s.LocalPath)
	if err != nil {
		return err
	}

	ext := fileExt(s.LocalPath)
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
			tplPath = filepath.Join(templateDir, tpl.(string))
		} else {
			tplPath = s.LocalPath
		}

		tmpl := template.New(filepath.Base(tplPath))
		tmpl = tmpl.Funcs(map[string]interface{}{
			"sort":   sortBy,
			"limit":  limit,
			"offset": offset,
			"filter": filter,
			"data":   loadData,
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
		tplData["Source"] = s
		tplData["Sources"] = sources

		if err := tmpl.Execute(dstFile, tplData); err != nil {
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
	for _, c := range s.Children {
		children = append(append(children, c), c.children()...)
	}
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
	case "LocalPath":
		val = s.LocalPath
	case "Filename":
		val = filepath.Base(s.LocalPath)
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
				source.LocalPath = filepath.Join(fullPath, child.Name())
			} else {
				childSource, err := loadSources(filepath.Join(path, child.Name()), baseDir)
				if err != nil {
					return source, err
				}
				if childSource.LocalPath != "" {
					source.Children = append(source.Children, childSource)
				}
			}
		}

	} else {
		source.Path = localToRemote(path)
		source.LocalPath = fullPath
	}

	if source.LocalPath == "" {
		source.LocalPath = fullPath
	} else {
		content, err := ioutil.ReadFile(source.LocalPath)
		if err != nil {
			return source, err
		}
		if _, ok := parseExtensions[fileExt(source.LocalPath)]; ok {
			if c, _ := parseContent(content, "---"); c != nil {
				if err := yaml.Unmarshal(c, &source.Meta); err != nil {
					return source, err
				}

				if p, ok := source.Meta["path"]; ok {
					source.Path = p.(string)
				}
			}
		}
	}

	return source, nil
}

func localToRemote(path string) string {
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

func sortBy(prop string, order string, sources []Source) []Source {
	sorted := make([]Source, len(sources))
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

func limit(limit int, sources []Source) []Source {
	if limit >= len(sources) {
		return sources
	}
	return sources[:limit]
}

func offset(offset int, sources []Source) []Source {
	if offset >= len(sources) {
		return []Source{}
	}
	return sources[offset:]
}

func filter(prop string, pattern string, sources []Source) []Source {
	filtered := []Source{}
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
