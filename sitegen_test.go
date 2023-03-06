package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func testSiteGen() *SiteGen {
	return newSiteGen("./site", "templates", "data", "src", "./public", "/", nil, true, true)
}

func TestGetSources(t *testing.T) {
	sg := testSiteGen()
	var tests = []struct {
		pattern string
		key     string
		want    int
	}{
		{"/img/promo.svg", "Path", 1},
		{"/news/*", "Path", 5},
		{"**about.html", "Filename", 1},
		{"index.html", "Filename", 1},
		{"Homepage", "Meta.title", 0},
		{"About Us", "Meta.title", 1},
	}

	for _, tt := range tests {
		testname := fmt.Sprintf("%s,%s", tt.pattern, tt.key)
		t.Run(testname, func(t *testing.T) {
			ans := len(sg.getSources(tt.key, tt.pattern))
			if ans != tt.want {
				t.Errorf("got %d, want %d", ans, tt.want)
			}
		})
	}
}

func TestOffset(t *testing.T) {
	sources := testSiteGen().sourceList()
	total := len(sources)
	var tests = []struct {
		offset int
		want   int
	}{
		{1, total - 1},
		{5, total - 5},
		{3, total - 3},
	}

	for _, tt := range tests {
		testname := fmt.Sprintf("%d", tt.offset)
		t.Run(testname, func(t *testing.T) {
			ans := len(offset(tt.offset, sources))
			if ans != tt.want {
				t.Errorf("got %d, want %d", ans, tt.want)
			}
		})
	}
}

func TestLimit(t *testing.T) {
	sources := testSiteGen().sourceList()
	total := len(sources)
	var tests = []struct {
		limit int
		want  int
	}{
		{1, 1},
		{2, 2},
		{total + 5, total},
		{total, total},
	}

	for _, tt := range tests {
		testname := fmt.Sprintf("%d", tt.limit)
		t.Run(testname, func(t *testing.T) {
			ans := len(limit(tt.limit, sources))
			if ans != tt.want {
				t.Errorf("got %d, want %d", ans, tt.want)
			}
		})
	}
}

func TestMapToList(t *testing.T) {
	sg := testSiteGen()

	data := sg.data("site.json")
	val, ok := data.(map[string]interface{})
	if !ok {
		t.Errorf("expected site.json map[string]interface got %T", data)
	}
	list := mapToList(val)
	var tests = []struct {
		key   string
		value string
	}{
		{"title", "Site Home"},
		{"description", "Site description"},
		{"url", "http://example.com"},
	}
	for i, tt := range tests {
		kv := list[i]
		if kv.Key != tt.key {
			t.Errorf("expects %s got %s", tt.key, kv.Key)
		}
		if kv.Value != tt.value {
			t.Errorf("expects %s got %s", tt.value, kv.Value)
		}
	}
}

func TestSort(t *testing.T) {
	sg := testSiteGen()
	sources := sg.getSources("Path", "/news/*")
	var tests = []struct {
		by    string
		order string
		want  []string
	}{
		{"Meta.date", "desc", []string{"2020-01-05", "2020-01-04", "2020-01-03", "2020-01-02", "2020-01-01"}},
		{"Meta.date", "asc", []string{"2020-01-01", "2020-01-02", "2020-01-03", "2020-01-04", "2020-01-05"}},
	}

	for _, tt := range tests {
		testname := fmt.Sprintf("sort sources %s,%s", tt.by, tt.order)
		t.Run(testname, func(t *testing.T) {
			ans := sortBy(tt.by, tt.order, sources)
			for i, w := range tt.want {
				a, ok := ans[i].(*Source)
				if !ok {
					t.Errorf("%s expects *Source got %T", tt.order, ans[i])
				} else if a.Meta["date"] != w {
					t.Errorf("%s got %v, want %v", tt.order, a.Meta["date"], w)
				}
			}
		})
	}

	var testJson = []struct {
		by    string
		order string
		want  []string
	}{
		{"name", "desc", []string{"News", "Home", "Contact", "About"}},
		{"name", "asc", []string{"About", "Contact", "Home", "News"}},
	}
	data := sg.data("links.json")
	for _, tt := range testJson {
		testname := fmt.Sprintf("sort links.json %s,%s", tt.by, tt.order)
		t.Run(testname, func(t *testing.T) {
			ans := sortBy(tt.by, tt.order, data)
			for i, w := range tt.want {
				a, ok := ans[i].(map[string]interface{})
				if !ok {
					t.Errorf("%s expects *Source got %T", tt.order, ans[i])
				} else if a[tt.by] != w {
					t.Errorf("%s got %v, want %v", tt.order, a[tt.by], w)
				}
			}
		})
	}

	var testSortedMap = []struct {
		by    string
		order string
		want  []string
	}{
		{"Key", "desc", []string{"url", "title", "description"}},
		{"Key", "asc", []string{"description", "title", "url"}},
	}
	val := sg.data("site.json").(map[string]interface{})
	list := mapToList(val)
	for _, tt := range testSortedMap {
		testname := fmt.Sprintf("sort map %s,%s", tt.by, tt.order)
		t.Run(testname, func(t *testing.T) {
			ans := sortBy(tt.by, tt.order, list)
			for i, w := range tt.want {
				a, ok := ans[i].(kv)
				if !ok {
					t.Errorf("%s expects kv got %T", tt.order, ans[i])
				} else if a.Key != w {
					t.Errorf("%s got %v, want %v", tt.order, a.Key, w)
				}
			}
		})
	}
}

func TestFilter(t *testing.T) {
	var d interface{}
	data := []byte(`[
		{"Page":"Abc"},
		{"Page":"A2c"},
		{"Page":"def"}
	]`)
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("failed unmarshal %v", err)
	}
	var tests = []struct {
		by      string
		pattern string
		want    []string
	}{
		{"Page", `^[A-Za-z]+$`, []string{"Abc", "def"}},
		{"Page", `\d+`, []string{"A2c"}},
	}
	for _, tt := range tests {
		testname := fmt.Sprintf("filter %s,%s", tt.by, tt.pattern)
		t.Run(testname, func(t *testing.T) {
			ans := filterBy(tt.by, tt.pattern, d)
			for i, w := range tt.want {
				a, ok := ans[i].(map[string]interface{})
				if !ok {
					t.Errorf("%s expects kv got %T", tt.pattern, ans[i])
				} else if a[tt.by] != w {
					t.Errorf("%s got %v, want %v", tt.pattern, a[tt.by], w)
				}
			}
		})
	}
}

func TestLocalToPath(t *testing.T) {
	sg := testSiteGen()
	var tests = []struct {
		source *Source
		want   string
	}{
		{sg.sources[filepath.Join(sg.sitePath, sg.sourceDir, "index.html")], "/"},
		{sg.sources[filepath.Join(sg.sitePath, sg.sourceDir, "css", "styles.css")], "/css/styles.css"},
		{sg.sources[filepath.Join(sg.sitePath, sg.sourceDir, "img", "promo.svg")], "/img/promo.svg"},
		{sg.sources[filepath.Join(sg.sitePath, sg.sourceDir, "404.html")], "/404.html"},
		{sg.sources[filepath.Join(sg.sitePath, sg.sourceDir, "news.html")], "/news"},
		{sg.sources[filepath.Join(sg.sitePath, sg.sourceDir, "contact.html")], "/contact"},
		{sg.sources[filepath.Join(sg.sitePath, sg.sourceDir, "news", "2020-01-01.html")], "/news/2020-01-01"},
	}

	for _, tt := range tests {
		testname := tt.source.Local
		t.Run(testname, func(t *testing.T) {
			ans := sg.localToPath(tt.source)
			if ans != tt.want {
				t.Errorf("got %s, want %s", ans, tt.want)
			}
		})
	}
}

func TestParseContent(t *testing.T) {
	var tests = []struct {
		input string
		sep   string
		want1 string
		want2 string
	}{
		{"ABC---DEF---GHI", "---", "DEF", "ABCGHI"},
		{`ABC
---
DEF: 123
---
GHI`, "---", "\nDEF: 123\n", "ABC\n\nGHI"},
		{`/*
---
serve: npm run build
build: npm run prod
---
*/`, "---", `
serve: npm run build
build: npm run prod
`, "/*\n\n*/"},
	}

	for _, tt := range tests {
		testname := tt.input
		t.Run(testname, func(t *testing.T) {
			ans1, ans2 := parseContent([]byte(tt.input), tt.sep)
			if string(ans1) != tt.want1 {
				t.Errorf("got %s, want1 %s", string(ans1), tt.want1)
			} else if string(ans2) != tt.want2 {
				t.Errorf("got %s, want2 %s", string(ans2), tt.want2)
			}
		})
	}
}

func TestData(t *testing.T) {
	sg := testSiteGen()
	var tests = []struct {
		data string
		want int
	}{
		{"links.json", 4},
		{"site.json", 2},
	}

	for _, tt := range tests {
		testname := tt.data
		t.Run(testname, func(t *testing.T) {
			sg.data(tt.data)
			// todo
		})
	}
}
