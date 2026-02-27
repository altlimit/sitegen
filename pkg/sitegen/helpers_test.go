package sitegen

import (
	"html/template"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileExt(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"file.go", ".go"},
		{"file.HTML", ".html"},
		{"file.Css", ".css"},
		{"file.tar.gz", ".gz"},
		{"noext", ""},
		{".hidden", ".hidden"},
		{"path/to/file.JS", ".js"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := fileExt(tt.input)
			if got != tt.want {
				t.Errorf("fileExt(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		sub, s string
		want   bool
	}{
		{"foo", "foobar", true},
		{"bar", "foobar", true},
		{"baz", "foobar", false},
		{"", "anything", true},
		{"x", "", false},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.sub+"_in_"+tt.s, func(t *testing.T) {
			got := contains(tt.sub, tt.s)
			if got != tt.want {
				t.Errorf("contains(%q, %q) = %v, want %v", tt.sub, tt.s, got, tt.want)
			}
		})
	}
}

func TestParseJSON(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  template.JS
	}{
		{"map", map[string]string{"a": "b"}, `{"a":"b"}`},
		{"slice", []int{1, 2, 3}, "[1,2,3]"},
		{"string", "hello", `"hello"`},
		{"nil", nil, "null"},
		{"number", 42, "42"},
		{"bool", true, "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseJSON(tt.input)
			if got != tt.want {
				t.Errorf("parseJSON(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValueOf(t *testing.T) {
	t.Run("map", func(t *testing.T) {
		m := map[string]interface{}{"name": "Alice", "age": 30}
		got := valueOf("name", reflect.ValueOf(m))
		if got != "Alice" {
			t.Errorf("got %q, want %q", got, "Alice")
		}
	})

	t.Run("map_missing_key", func(t *testing.T) {
		m := map[string]interface{}{"name": "Alice"}
		got := valueOf("missing", reflect.ValueOf(m))
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("map_non_string_value", func(t *testing.T) {
		m := map[string]interface{}{"count": 42}
		got := valueOf("count", reflect.ValueOf(m))
		if got != "" {
			t.Errorf("got %q, want empty string for non-string value", got)
		}
	})

	t.Run("kv_Key", func(t *testing.T) {
		v := kv{Key: "mykey", Value: "myval"}
		got := valueOf("Key", reflect.ValueOf(v))
		if got != "mykey" {
			t.Errorf("got %q, want %q", got, "mykey")
		}
	})

	t.Run("kv_Value_string", func(t *testing.T) {
		v := kv{Key: "k", Value: "stringval"}
		got := valueOf("Value", reflect.ValueOf(v))
		if got != "stringval" {
			t.Errorf("got %q, want %q", got, "stringval")
		}
	})

	t.Run("kv_Value_non_string", func(t *testing.T) {
		v := kv{Key: "k", Value: 123}
		got := valueOf("Value", reflect.ValueOf(v))
		if got != "" {
			t.Errorf("got %q, want empty string for non-string kv value", got)
		}
	})

	t.Run("kv_Value_nested", func(t *testing.T) {
		v := kv{Key: "k", Value: map[string]interface{}{"nested": "deep"}}
		got := valueOf("Value.nested", reflect.ValueOf(v))
		if got != "deep" {
			t.Errorf("got %q, want %q", got, "deep")
		}
	})

	t.Run("source", func(t *testing.T) {
		sg := &SiteGen{BasePath: "/", SourceDir: "src", SitePath: "/site"}
		s := &Source{
			Name:  "test.html",
			Local: "/site/src/test.html",
			Path:  "/test",
			Ext:   ".html",
			Meta:  map[string]interface{}{"title": "Hello"},
			sg:    sg,
		}
		got := valueOf("Path", reflect.ValueOf(s))
		if got != "/test" {
			t.Errorf("got %q, want %q", got, "/test")
		}
		got = valueOf("Meta.title", reflect.ValueOf(s))
		if got != "Hello" {
			t.Errorf("got %q, want %q", got, "Hello")
		}
	})

	t.Run("unknown_type", func(t *testing.T) {
		got := valueOf("anything", reflect.ValueOf(42))
		if got != "" {
			t.Errorf("got %q, want empty string for unknown type", got)
		}
	})
}

func TestPages(t *testing.T) {
	t.Run("no_pages", func(t *testing.T) {
		s := &Source{TotalPages: 1, CurrentPage: 1, Path: "/blog"}
		result := pages(s)
		if result != nil {
			t.Errorf("expected nil for TotalPages=1, got %v", result)
		}
	})

	t.Run("zero_pages", func(t *testing.T) {
		s := &Source{TotalPages: 0, CurrentPage: 0, Path: "/blog"}
		result := pages(s)
		if result != nil {
			t.Errorf("expected nil for TotalPages=0, got %v", result)
		}
	})

	t.Run("multiple_pages", func(t *testing.T) {
		s := &Source{TotalPages: 3, CurrentPage: 2, Path: "/blog"}
		result := pages(s)
		if len(result) != 3 {
			t.Fatalf("expected 3 pages, got %d", len(result))
		}

		// Page 1: path="/blog", not active
		if result[0].Path != "/blog" || result[0].Page != 1 || result[0].Active {
			t.Errorf("page 1: got %+v", result[0])
		}
		// Page 2: path="/blog/2", active
		if result[1].Path != "/blog/2" || result[1].Page != 2 || !result[1].Active {
			t.Errorf("page 2: got %+v", result[1])
		}
		// Page 3: path="/blog/3", not active
		if result[2].Path != "/blog/3" || result[2].Page != 3 || result[2].Active {
			t.Errorf("page 3: got %+v", result[2])
		}
	})
}

func TestSourcePath(t *testing.T) {
	sg := &SiteGen{PublicPath: "./public"}

	tests := []struct {
		name string
		src  *Source
		want string
	}{
		{
			"html_file",
			&Source{Ext: ".html", Path: "/about"},
			filepath.Join("public", "about", "index.html"),
		},
		{
			"htm_file",
			&Source{Ext: ".htm", Path: "/contact"},
			filepath.Join("public", "contact", "index.html"),
		},
		{
			"html_with_extension_in_path",
			&Source{Ext: ".html", Path: "/page.html"},
			filepath.Join("public", "page.html"),
		},
		{
			"css_file",
			&Source{Ext: ".css", Path: "/css/style.css"},
			filepath.Join("public", "css", "style.css"),
		},
		{
			"image_file",
			&Source{Ext: ".jpg", Path: "/img/photo.jpg"},
			filepath.Join("public", "img", "photo.jpg"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sg.sourcePath(tt.src)
			if got != tt.want {
				t.Errorf("sourcePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSourceValue(t *testing.T) {
	sg := &SiteGen{
		BasePath:  "/",
		SourceDir: "src",
		SitePath:  "/site",
	}
	s := &Source{
		Name:  "test.html",
		Local: filepath.Join("/site", "src", "blog", "post.html"),
		Path:  "/blog/post",
		Ext:   ".html",
		Meta:  map[string]interface{}{"title": "My Post", "date": "2026-01-01"},
		sg:    sg,
	}

	tests := []struct {
		prop string
		want string
	}{
		{"Path", "/blog/post"},
		{"Local", s.Local},
		{"Filename", "post.html"},
		{"Ext", ".html"},
		{"Meta.title", "My Post"},
		{"Meta.date", "2026-01-01"},
		{"Meta.missing", "<nil>"},
		{"Unknown", ""},
	}
	for _, tt := range tests {
		t.Run(tt.prop, func(t *testing.T) {
			got := s.Value(tt.prop)
			if got != tt.want {
				t.Errorf("Value(%q) = %q, want %q", tt.prop, got, tt.want)
			}
		})
	}
}

func TestSourceValueRelPath(t *testing.T) {
	sg := &SiteGen{
		BasePath:  "/",
		SourceDir: "src",
		SitePath:  "/site",
	}
	s := &Source{
		Local: filepath.Join("/site", "src", "blog", "post.html"),
		Ext:   ".html",
		Meta:  map[string]interface{}{},
		sg:    sg,
	}

	got := s.Value("RelPath")
	want := "blog/post.html"
	if got != want {
		t.Errorf("Value(\"RelPath\") = %q, want %q", got, want)
	}
}
