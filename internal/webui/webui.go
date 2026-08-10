// Package webui embeds the built web/ frontend (React + TypeScript, see
// web/README.md) into the bop binary so "bop controller" can serve it
// directly, with no separate deploy or Node.js runtime required. dist/ is
// vite build's output (web/vite.config.ts writes here) - regenerate it
// with `make build-web` after changing anything under web/, and commit
// the result, the same discipline as any other generated/vendored file:
// `go build`/`go install` must work standalone without Node.js installed.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
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

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(assets, assetPath(r.URL.Path)); err != nil {
			r = withPath(r, "/")
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}

func assetPath(urlPath string) string {
	if urlPath == "" || urlPath == "/" {
		return "index.html"
	}
	return urlPath[1:] // fs.Stat wants a path relative to the embedded root, no leading slash.
}

func withPath(r *http.Request, path string) *http.Request {
	r2 := r.Clone(r.Context())
	r2.URL.Path = path
	return r2
}
