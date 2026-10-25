package portal

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SPA serves the Vite build from disk. The portal deliberately keeps the
// frontend out of the Go binary: `pnpm build` produces portal/dist, and
// portal.web_dir points at it. Client routes fall back to index.html so a
// refresh survives at any depth.
type SPA struct {
	dir string
	// prefix is the URL prefix this handler owns. NewSPA owns /portal/;
	// NewRootSPA owns / and rejects the gateway's reserved prefixes.
	prefix string
	// bareRedirect sends the prefix without its trailing slash to this target.
	// NewSPA keeps the historical /portal -> /portal/ redirect.
	bareRedirect string
	reserved     map[string]struct{}
	// missingFile404 makes a path with a file extension 404 instead of
	// returning the shell, so an old /portal/assets/... request cannot be
	// mistaken for a valid page.
	missingFile404 bool
}

// NewSPA returns a handler for the built portal under /portal/. A missing
// directory is not an error: the handler explains how to build the frontend
// instead of crashing the gateway.
func NewSPA(dir string) http.Handler {
	return &SPA{dir: dir, prefix: "/portal/", bareRedirect: "/portal/"}
}

// NewRootSPA returns the root site handler. It serves the same build at /,
// /login and /portal, and refuses the prefixes owned by the gateway's API,
// MCP, artifact, voice and health endpoints.
func NewRootSPA(dir string) http.Handler {
	return &SPA{
		dir:            dir,
		prefix:         "/",
		reserved:       map[string]struct{}{"mcp": {}, "sse": {}, "api": {}, "voice": {}, "artifacts": {}, "healthz": {}},
		missingFile404: true,
	}
}

func (s *SPA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if s.bareRedirect != "" && r.URL.Path == strings.TrimSuffix(s.prefix, "/") {
		http.Redirect(w, r, s.bareRedirect, http.StatusMovedPermanently)
		return
	}
	if s.prefix != "/" && !strings.HasPrefix(r.URL.Path, s.prefix) {
		http.NotFound(w, r)
		return
	}
	if s.reservedPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, s.prefix)
	if s.prefix == "/" {
		rel = strings.TrimPrefix(r.URL.Path, "/")
	}
	clean := path.Clean("/" + rel)
	if clean == "/" || clean == "" {
		s.serveIndex(w, r)
		return
	}
	// Only real files are served directly. Everything else is a client route
	// and must return the SPA shell, unless it looks like a stale asset.
	name := strings.TrimPrefix(clean, "/")
	if file, err := os.Stat(filepath.Join(s.dir, filepath.FromSlash(name))); err == nil && !file.IsDir() {
		if strings.HasPrefix(clean, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeFile(w, r, filepath.Join(s.dir, filepath.FromSlash(name)))
		return
	}
	if s.missingFile404 && path.Ext(clean) != "" {
		http.NotFound(w, r)
		return
	}
	s.serveIndex(w, r)
}

func (s *SPA) reservedPath(p string) bool {
	if len(s.reserved) == 0 {
		return false
	}
	segment, _, _ := strings.Cut(strings.TrimPrefix(p, "/"), "/")
	_, ok := s.reserved[segment]
	return ok
}

func (s *SPA) serveIndex(w http.ResponseWriter, r *http.Request) {
	index := filepath.Join(s.dir, "index.html")
	if info, err := os.Stat(index); err != nil || info.IsDir() {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintf(w, missingBuildPage, html.EscapeString(s.dir))
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, index)
}

const missingBuildPage = `<!doctype html>
<html lang="zh-CN"><meta charset="utf-8">
<title>Open Anywhere · 门户尚未构建</title>
<body style="font-family:system-ui;background:#111318;color:#f2f5fa;padding:48px;line-height:1.7">
<h1>门户前端尚未构建</h1>
<p>请先构建前端静态资源：</p>
<pre style="background:#1b1f27;padding:16px;border-radius:10px">cd portal
pnpm install
pnpm build</pre>
<p>然后把 <code>portal.web_dir</code> 指向构建产物（默认 <code>%s</code>）。</p>
</body></html>`

// logMissingWebDir records a one-line hint at startup.
func logMissingWebDir(dir string) {
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		log.Printf("portal: frontend not built in %s; run `cd portal && pnpm install && pnpm build`", dir)
	}
}
