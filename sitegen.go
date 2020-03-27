package main

import (
	"html/template"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v2"
)

// Source represents a resource
type Source struct {
	Children  []Source
	LocalPath string
	Path      string
	Meta      map[string]interface{}
}

func (s *Source) ext() string {
	return strings.ToLower(filepath.Ext(s.LocalPath))
}

func (s *Source) build(outputDir string, sources []Source) error {
	if s.Path == "" {
		return nil
	}

	src, err := ioutil.ReadFile(s.LocalPath)
	if err != nil {
		return err
	}

	switch s.ext() {
	case ".html", ".htm":
		if err := os.MkdirAll(filepath.Join(outputDir, s.Path), os.ModePerm); err != nil {
			return err
		}

		dstPath := filepath.Join(outputDir, s.Path, "index.html")
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
		})

		tmpl, err = tmpl.ParseFiles(templates...)
		if err != nil {
			return err
		}

		if strings.HasPrefix(string(src), "---") {
			src = []byte(strings.SplitN(string(src), "---", 3)[2])
		}

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
			val = s.Meta[prop[5:]].(string)
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
		c := string(content)
		idx := strings.Index(c, "---")
		if idx >= 0 {
			c = c[idx+3:]
			idx = strings.Index(c, "---")
			if idx >= 0 {
				if err := yaml.Unmarshal([]byte(c[:idx]), &source.Meta); err != nil {
					return source, err
				}
			}
		}
	}

	return source, nil
}

func localToRemote(path string) string {
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".html", ".htm":
		path = strings.TrimSuffix(path, ext)
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
