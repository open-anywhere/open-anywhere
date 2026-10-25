package portal

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SPA serves the Vite build. By default it is read from disk: `pnpm build`
// produces portal/dist, and portal.web_dir points at it. A `-tags portalembed`
// build can carry the same output inside the binary instead, selected by
// portal.web_embedded; the empty dir then means "serve the embedded copy".
// Client routes fall back to index.html so a refresh survives at any depth.
type SPA struct {
	fsys fs.FS
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

// rootReservedPrefixes belong to the gateway's API, MCP, artifact, voice and
// health endpoints; the site shell must never answer for them.
var rootReservedPrefixes = map[string]struct{}{
	"mcp": {}, "sse": {}, "api": {}, "voice": {}, "artifacts": {}, "healthz": {},
}

// NewSPA returns a handler for the built portal under /portal/. A missing
// build is not an error: the handler explains how to build the frontend
// instead of crashing the gateway.
func NewSPA(dir string) http.Handler {
	return newSPA(resolveWebFS(dir), "/portal/", "/portal/", nil, false)
}

// NewRootSPA returns the root site handler. It serves the same build at /,
// /login and /portal, and refuses the prefixes owned by the gateway's API,
// MCP, artifact, voice and health endpoints.
func NewRootSPA(dir string) http.Handler {
	return newSPA(resolveWebFS(dir), "/", "", rootReservedPrefixes, true)
}

// EmbeddedWebAvailable reports whether this build carries the portal frontend.
func EmbeddedWebAvailable() bool { return embeddedWebFS() != nil }

// resolveWebFS prefers the compiled-in build when web_dir is empty.
func resolveWebFS(dir string) fs.FS {
	if dir != "" {
		return os.DirFS(dir)
	}
	return embeddedWebFS()
}

func newSPA(fsys fs.FS, prefix, bareRedirect string, reserved map[string]struct{}, missingFile404 bool) http.Handler {
	return &SPA{
		fsys:           fsys,
		prefix:         prefix,
		bareRedirect:   bareRedirect,
		reserved:       reserved,
		missingFile404: missingFile404,
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
	if s.fsys != nil {
		if file, err := fs.Stat(s.fsys, name); err == nil && !file.IsDir() {
			if strings.HasPrefix(clean, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeFileFS(w, r, s.fsys, name)
			return
		}
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
	if s.fsys != nil {
		if info, err := fs.Stat(s.fsys, "index.html"); err == nil && !info.IsDir() {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, s.fsys, "index.html")
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = fmt.Fprint(w, missingBuildPage)
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
<p>构建产物会自动同步到 <code>internal/gateway/portal/webdist</code>：用
<code>-tags portalembed</code> 重新编译即可内嵌进二进制，或把
<code>portal.web_dir</code> 指向 <code>portal/dist</code> 从磁盘加载。</p>
</body></html>`

// logMissingWebDir records a one-line hint at startup.
func logMissingWebDir(dir string) {
	if dir == "" {
		if embeddedWebFS() == nil {
			log.Printf("portal: no frontend available; run `cd portal && pnpm install && pnpm build`, then rebuild with -tags portalembed or set portal.web_dir")
		}
		return
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		log.Printf("portal: frontend not built in %s; run `cd portal && pnpm install && pnpm build`", dir)
	}
}
