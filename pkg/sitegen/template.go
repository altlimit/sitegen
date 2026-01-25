package sitegen

import (
	"fmt"
	"path/filepath"
	texttemplate "text/template"
)

// LoadTemplate parses all templates of a specific type (ext) and stores them in cache
func (sg *SiteGen) LoadTemplate(t string) error {
	// Dummy funcs to allow parsing
	// We need to provide all funcs that might be used in base templates
	funcs := sg.tplFuncs()
	funcs["page"] = func(source, path string) string { return "" }
	funcs["paginate"] = func(limit int, list interface{}) interface{} { return list }

	tpl := texttemplate.New("base").Funcs(funcs)

	tplFiles, err := filepath.Glob(filepath.Join(sg.SitePath, sg.TemplateDir, "*."+t))
	if err != nil {
		return err
	}
	if len(tplFiles) > 0 {
		tpl, err = tpl.ParseFiles(tplFiles...)
		if err != nil {
			return fmt.Errorf("LoadTemplate ParseFiles error: %v", err)
		}
	}

	sg.TplCache[t] = tpl
	return nil
}
