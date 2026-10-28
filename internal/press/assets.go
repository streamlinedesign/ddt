package press

import (
	"encoding/base64"
	"fmt"
	"html/template"
	"io/fs"
	"mime"
	"path"
	"regexp"
	"strings"
)

// Assets are the styles and scripts an edition carries inside itself.
type Assets struct {
	CSS     template.CSS
	EarlyJS template.JS
	PageJS  template.JS
	// Words is Riječililica's guess list and Matter the physics for Izvori, both carried as text
	// so the edition works opened from disk; the page starts Matter only when Izvori is in view.
	Words  string
	Matter template.HTML
}

var cssURL = regexp.MustCompile(`url\(\s*(['"]?)([^'")]+)['"]?\s*\)`)

func (p *Press) assets() (Assets, error) {
	var a Assets
	css, err := p.inlineCSS("static/fonts.css", "static/index.css")
	if err != nil {
		return a, err
	}
	early, err := p.join("static/js/early.js")
	if err != nil {
		return a, err
	}
	page, err := p.join("static/js/page.js", "static/js/images.js", "static/js/wall.js", "static/js/game.js", "static/js/izvori.js")
	if err != nil {
		return a, err
	}
	words, err := fs.ReadFile(p.Web, "static/rijeci.txt")
	if err != nil {
		return a, err
	}
	matter, err := p.join("static/vendor/matter-0.20.0.min.js")
	if err != nil {
		return a, err
	}
	a.Words = strings.Join(strings.Fields(string(words)), " ")
	a.Matter = template.HTML(strings.ReplaceAll(matter, "<!--", `<\!--`))
	a.CSS = template.CSS(css)
	a.EarlyJS = template.JS(early)
	a.PageJS = template.JS(page)
	return a, nil
}

func (p *Press) join(names ...string) (string, error) {
	var b strings.Builder
	for _, n := range names {
		data, err := fs.ReadFile(p.Web, n)
		if err != nil {
			return "", err
		}
		b.WriteString(strings.ReplaceAll(string(data), "</script", `<\/script`))
		b.WriteString("\n")
	}
	return b.String(), nil
}

// inlineCSS joins stylesheets and turns every local url() into a data URI, so fonts and icons travel with the page.
func (p *Press) inlineCSS(names ...string) (string, error) {
	var b strings.Builder
	for _, n := range names {
		data, err := fs.ReadFile(p.Web, n)
		if err != nil {
			return "", err
		}
		dir := path.Dir(n)
		var bad error
		out := cssURL.ReplaceAllStringFunc(string(data), func(m string) string {
			ref := cssURL.FindStringSubmatch(m)[2]
			if strings.HasPrefix(ref, "data:") || strings.Contains(ref, "://") || strings.HasPrefix(ref, "#") {
				return m
			}
			uri, err := p.dataURI(path.Join(dir, ref))
			if err != nil {
				bad = err
				return m
			}
			return "url(" + uri + ")"
		})
		if bad != nil {
			return "", bad
		}
		b.WriteString(out)
		b.WriteString("\n")
	}
	return strings.ReplaceAll(b.String(), "</style", `<\/style`), nil
}

func (p *Press) dataURI(name string) (string, error) {
	data, err := fs.ReadFile(p.Web, name)
	if err != nil {
		return "", fmt.Errorf("inline %s: %w", name, err)
	}
	kind := mime.TypeByExtension(path.Ext(name))
	switch path.Ext(name) {
	case ".woff2":
		kind = "font/woff2"
	case ".svg":
		kind = "image/svg+xml"
	}
	if kind == "" {
		kind = "application/octet-stream"
	}
	return "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

var iconName = regexp.MustCompile(`^[a-z-]+$`)

// weatherIcons gives the styles for the weather icons an edition shows, and only those:
// all of them would add 20 KB to every edition.
func (p *Press) weatherIcons(snap Snapshot) (template.CSS, error) {
	seen := map[string]bool{}
	var b strings.Builder
	for _, f := range snap.Weather {
		for _, part := range f.Parts {
			if seen[part.Icon] || !iconName.MatchString(part.Icon) {
				continue
			}
			seen[part.Icon] = true
			uri, err := p.dataURI("static/icons/weather/" + part.Icon + ".svg")
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, ".wx--%s{--wx:url(%s)}\n", part.Icon, uri)
		}
	}
	return template.CSS(b.String()), nil
}
