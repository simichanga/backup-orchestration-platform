// Package webui embeds the built web/ frontend (React + TypeScript, see
// web/README.md) into the bop binary so "bop controller" can serve it
// directly, with no separate deploy or Node.js runtime required. dist/ is
// vite build's output (web/vite.config.ts writes here) - regenerate it
// with `make build-web` after changing anything under web/, and commit
// the result, the same discipline as any other generated/vendored file:
// `go build`/`go install` must work standalone without Node.js installed.
package webui

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"regexp"
	"time"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the embedded frontend as a single-page app: real static
// assets (JS/CSS/fonts/favicon) are served as-is, and any other path -
// anything the frontend's own client-side router owns, like /jobs/abc123 -
// falls back to index.html so a hard refresh or a shared deep link works.
// This only serves "/" and below; the caller is responsible for making
// sure more specific patterns like "/v1/..." are registered first, since
// Go 1.22+'s http.ServeMux already prefers the more specific match.
func Handler() (http.Handler, error) {
	assets, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(assets))
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return nil, err
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := assetPath(r.URL.Path)
		if p != "index.html" {
			if _, err := fs.Stat(assets, p); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		serveIndex(w, r, index)
	}), nil
}

// A reverse proxy that mounts the UI under a sub-path (Traefik's stripPrefix
// sets this header) gets a matching <base href>, so relative asset and API
// URLs resolve under that path. Anything that isn't a plain path is ignored.
var prefixPattern = regexp.MustCompile(`^(/[A-Za-z0-9._-]+)+$`)

func serveIndex(w http.ResponseWriter, r *http.Request, index []byte) {
	base := "/"
	if prefix := r.Header.Get("X-Forwarded-Prefix"); prefixPattern.MatchString(prefix) {
		base = prefix + "/"
	}
	page := bytes.Replace(index, []byte("<head>"), []byte(`<head><base href="`+base+`">`), 1)
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(page))
}

func assetPath(urlPath string) string {
	if urlPath == "" || urlPath == "/" {
		return "index.html"
	}
	return urlPath[1:] // fs.Stat wants a path relative to the embedded root, no leading slash.
}
