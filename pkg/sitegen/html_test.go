package sitegen

import (
	"strings"
	"testing"
)

func TestRewriteHTMLImages_WebpDisabled(t *testing.T) {
	body := []byte(`<html><body><img src="/img/photo.jpg"></body></html>`)
	got, err := rewriteHTMLImages(body, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Errorf("expected body unchanged when webp disabled, got %s", got)
	}
}

func TestRewriteHTMLImages_NoImages(t *testing.T) {
	body := []byte(`<html><body><p>Hello world</p></body></html>`)
	got, err := rewriteHTMLImages(body, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "Hello world") {
		t.Errorf("expected content preserved, got %s", got)
	}
	if strings.Contains(string(got), "<picture>") {
		t.Error("unexpected <picture> tag when no images present")
	}
}

func TestRewriteHTMLImages_JPG(t *testing.T) {
	body := []byte(`<img src="/img/photo.jpg">`)
	got, err := rewriteHTMLImages(body, true)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "<picture>") {
		t.Error("expected <picture> wrapper")
	}
	if !strings.Contains(s, `<source srcset="/img/photo.webp" type="image/webp">`) {
		t.Errorf("expected webp source tag, got %s", s)
	}
	if !strings.Contains(s, `src="/img/photo.jpg"`) {
		t.Error("expected original img tag preserved")
	}
	if !strings.Contains(s, "</picture>") {
		t.Error("expected closing </picture>")
	}
}

func TestRewriteHTMLImages_JPEG(t *testing.T) {
	body := []byte(`<img src="/img/photo.jpeg">`)
	got, err := rewriteHTMLImages(body, true)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "<picture>") {
		t.Error("expected <picture> wrapper for .jpeg")
	}
	if !strings.Contains(s, "/img/photo.webp") {
		t.Error("expected webp source for .jpeg")
	}
}

func TestRewriteHTMLImages_PNG(t *testing.T) {
	body := []byte(`<img src="/img/icon.png">`)
	got, err := rewriteHTMLImages(body, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "<picture>") {
		t.Error("expected <picture> wrapper for .png")
	}
	if !strings.Contains(string(got), "/img/icon.webp") {
		t.Error("expected webp source for .png")
	}
}

func TestRewriteHTMLImages_SVG_Unchanged(t *testing.T) {
	body := []byte(`<img src="/img/logo.svg">`)
	got, err := rewriteHTMLImages(body, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "<picture>") {
		t.Error("SVG should not be wrapped in <picture>")
	}
}

func TestRewriteHTMLImages_GIF_Unchanged(t *testing.T) {
	body := []byte(`<img src="/img/anim.gif">`)
	got, err := rewriteHTMLImages(body, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "<picture>") {
		t.Error("GIF should not be wrapped in <picture>")
	}
}

func TestRewriteHTMLImages_MixedImages(t *testing.T) {
	body := []byte(`<div><img src="/a.jpg"><img src="/b.svg"><img src="/c.png"></div>`)
	got, err := rewriteHTMLImages(body, true)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	// jpg and png should be wrapped, svg should not
	if strings.Count(s, "<picture>") != 2 {
		t.Errorf("expected 2 <picture> tags, got %d in: %s", strings.Count(s, "<picture>"), s)
	}
}

func TestRewriteHTMLImages_CaseInsensitive(t *testing.T) {
	body := []byte(`<img src="/img/PHOTO.JPG">`)
	got, err := rewriteHTMLImages(body, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "<picture>") {
		t.Error("expected case-insensitive matching for .JPG")
	}
}

func TestRewriteHTMLImages_PreservesOtherHTML(t *testing.T) {
	body := []byte(`<html><head><title>Test</title></head><body><p>Hello</p><img src="/x.jpg"><a href="/">Link</a></body></html>`)
	got, err := rewriteHTMLImages(body, true)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "<title>") {
		t.Error("expected <title> preserved")
	}
	if !strings.Contains(s, "Hello") {
		t.Error("expected paragraph preserved")
	}
	if !strings.Contains(s, `href="/"`) {
		t.Error("expected link preserved")
	}
}
