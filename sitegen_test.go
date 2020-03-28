package main

import (
	"fmt"
	"testing"
)

func siteSources() []Source {
	s, err := loadSources("/", "./site/src")
	if err != nil {
		panic(err)
	}
	return s.sources()
}

func TestFilter(t *testing.T) {
	sources := siteSources()
	var tests = []struct {
		pattern string
		key     string
		want    int
	}{
		{"/img/promo.svg", "Path", 1},
		{"/news/*", "Path", 4},
		{"**about.html", "Filename", 1},
		{"index.html", "Filename", 1},
		{"Homepage", "Meta.title", 0},
		{"About Us", "Meta.title", 1},
	}

	for _, tt := range tests {
		testname := fmt.Sprintf("%s,%s", tt.pattern, tt.key)
		t.Run(testname, func(t *testing.T) {
			ans := len(filter(tt.key, tt.pattern, sources))
			if ans != tt.want {
				t.Errorf("got %d, want %d", ans, tt.want)
			}
		})
	}
}

func TestOffset(t *testing.T) {
	sources := siteSources()
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
	sources := siteSources()
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

func TestSort(t *testing.T) {
	sources := filter("Path", "/news/*", siteSources())
	var tests = []struct {
		by    string
		order string
		want  []string
	}{
		{"Meta.date", "desc", []string{"2020-01-04", "2020-01-03", "2020-01-02", "2020-01-01"}},
		{"Meta.date", "asc", []string{"2020-01-01", "2020-01-02", "2020-01-03", "2020-01-04"}},
	}

	for _, tt := range tests {
		testname := fmt.Sprintf("%s,%s", tt.by, tt.order)
		t.Run(testname, func(t *testing.T) {
			ans := sortBy(tt.by, tt.order, sources)
			for i, w := range tt.want {
				if ans[i].Meta["date"] != w {
					t.Errorf("got %v, want %v", ans, tt.want)
				}
			}
		})
	}
}

func TestLocalToRemote(t *testing.T) {
	var tests = []struct {
		local string
		want  string
	}{
		{"index.html", "/"},
		{"/hello/index.html", "/hello/"},
		{"news.html", "/news"},
		{"/css/style.css", "/css/style.css"},
		{"logo.png", "/logo.png"},
		{"/news/2020.html", "/news/2020"},
	}

	for _, tt := range tests {
		testname := fmt.Sprintf("%s", tt.local)
		t.Run(testname, func(t *testing.T) {
			ans := localToRemote(tt.local)
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
		testname := fmt.Sprintf("%s", tt.input)
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
