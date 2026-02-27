package sitegen

import (
	"bytes"
	"io"
	"strings"

	"golang.org/x/net/html"
)

func rewriteHTMLImages(body []byte, useWebp bool) ([]byte, error) {
	if !useWebp {
		return body, nil
	}

	r := bytes.NewReader(body)
	z := html.NewTokenizer(r)
	var buf bytes.Buffer

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			err := z.Err()
			if err == io.EOF {
				break
			}
			return nil, err
		}

		tok := z.Token()

		if tok.Type == html.StartTagToken || tok.Type == html.SelfClosingTagToken {
			if tok.Data == "img" {
				var src string
				for _, attr := range tok.Attr {
					if attr.Key == "src" {
						src = attr.Val
						break
					}
				}

				lowerSrc := strings.ToLower(src)
				if strings.HasSuffix(lowerSrc, ".jpg") || strings.HasSuffix(lowerSrc, ".jpeg") || strings.HasSuffix(lowerSrc, ".png") {
					webpSrc := src[:strings.LastIndex(src, ".")] + ".webp"

					buf.WriteString("<picture>")
					buf.WriteString(`<source srcset="`)
					buf.WriteString(html.EscapeString(webpSrc))
					buf.WriteString(`" type="image/webp">`)

					buf.WriteString(tok.String())

					buf.WriteString("</picture>")
					continue
				}
			}
		}

		buf.WriteString(tok.String())
	}

	return buf.Bytes(), nil
}
