package sitegen

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
)

type Source struct {
	Name  string
	Local string
	Path  string
	Meta  map[string]interface{}

	Ext     string
	Ctype   string
	content []byte
	sg      *SiteGen
	page    int
	pages   int
	path    string
}

func (s *Source) ReloadContent() []byte {
	s.content = nil
	return s.LoadContent()
}

func (s *Source) LoadContent() []byte {
	if s.content == nil {
		s.page = 0
		s.pages = 0
		var (
			meta    []byte
			content []byte
		)
		c, err := os.ReadFile(s.Local)
		if err != nil {
			log.Println("Source loading failed ", err)
			return nil
		}
		_, txtCtype := parseCtype[s.Ctype]
		if txtCtype {
			meta, content = ParseContent(c, "---")
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
	s.Path = s.sg.LocalToPath(s)
	return s.content
}

func (s *Source) Value(prop string) string {
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

func ParseContent(content []byte, sep string) ([]byte, []byte) {
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
