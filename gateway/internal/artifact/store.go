// Package artifact validates and stores build output without executing it.
package artifact

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/open-anywhere/open-anywhere/gateway/internal/application"
	"github.com/open-anywhere/open-anywhere/gateway/internal/openapi"
	"go.yaml.in/yaml/v3"
)

const MaxCompressed int64 = 100 << 20
const maxExpanded int64 = 300 << 20

type Store struct{ Dir, PublicURL string }

func (s Store) Save(reader io.Reader, expected int64) (application.BuildProduct, error) {
	var result application.BuildProduct
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return result, err
	}
	file, err := os.CreateTemp(s.Dir, ".artifact-*")
	if err != nil {
		return result, err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(reader, MaxCompressed+1))
	if err != nil {
		return result, err
	}
	if n > MaxCompressed {
		return result, fmt.Errorf("ARTIFACT_TOO_LARGE")
	}
	if expected > 0 && n != expected {
		return result, fmt.Errorf("ARTIFACT_CORRUPT: size mismatch")
	}
	if _, err = file.Seek(0, 0); err != nil {
		return result, err
	}
	name, doc, err := validate(file)
	if err != nil {
		return result, err
	}
	if err = file.Sync(); err != nil {
		return result, err
	}
	if err = file.Close(); err != nil {
		return result, err
	}
	var key [24]byte
	if _, err = rand.Read(key[:]); err != nil {
		return result, err
	}
	filename := hex.EncodeToString(key[:]) + ".tar.gz"
	if err = os.Rename(file.Name(), filepath.Join(s.Dir, filename)); err != nil {
		return result, err
	}
	result = application.BuildProduct{Name: name, Document: doc, Digest: hex.EncodeToString(hash.Sum(nil)), Path: filepath.Join(s.Dir, filename), URL: strings.TrimRight(s.PublicURL, "/") + "/artifacts/" + filename}
	return result, nil
}
func validate(reader io.Reader) (string, json.RawMessage, error) {
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return "", nil, err
	}
	defer gz.Close()
	limited := &io.LimitedReader{R: gz, N: maxExpanded + 1}
	tr := tar.NewReader(limited)
	files := map[string]bool{}
	regular := map[string]bool{}
	var launch, doc []byte
	var packageJSON []byte
	count := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, err
		}
		count++
		if count > 10000 {
			return "", nil, fmt.Errorf("too many archive entries")
		}
		raw := h.Name
		name := path.Clean(strings.TrimPrefix(raw, "./"))
		if strings.HasPrefix(raw, "/") || strings.ContainsAny(raw, "\\\x00:") {
			return "", nil, fmt.Errorf("invalid archive path")
		}
		for _, part := range strings.Split(raw, "/") {
			if part == ".." {
				return "", nil, fmt.Errorf("archive traversal")
			}
		}
		if name == "." && h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeDir && h.Typeflag != tar.TypeReg {
			return "", nil, fmt.Errorf("archive links and special files forbidden")
		}
		if files[name] {
			return "", nil, fmt.Errorf("duplicate archive path")
		}
		files[name] = true
		regular[name] = h.Typeflag == tar.TypeReg
		if h.Size < 0 || h.Size > maxExpanded {
			return "", nil, fmt.Errorf("invalid file size")
		}
		if name == "launch.json" || name == "openapi.yaml" || name == "app/package.json" {
			b, err := io.ReadAll(io.LimitReader(tr, openapi.Limit+1))
			if err != nil || len(b) > openapi.Limit {
				return "", nil, fmt.Errorf("manifest too large")
			}
			if name == "launch.json" {
				launch = b
			} else if name == "openapi.yaml" {
				doc = b
			} else {
				packageJSON = b
			}
		}
	}
	// Consume gzip trailer to verify checksum even after tar end markers.
	_, err = io.Copy(io.Discard, limited)
	if err != nil || limited.N <= 0 {
		return "", nil, fmt.Errorf("invalid or oversized gzip archive")
	}
	if packageJSON != nil && !json.Valid(packageJSON) {
		return "", nil, fmt.Errorf("invalid package.json")
	}
	var l map[string]any
	if json.Unmarshal(launch, &l) != nil {
		return "", nil, fmt.Errorf("invalid launch.json")
	}
	_, fixedPort := l["port"]
	if l["runtimeExecutable"] != "node" || fixedPort {
		return "", nil, fmt.Errorf("invalid runtime or fixed port")
	}
	if h, ok := l["healthPath"]; ok && h != "/health" {
		return "", nil, fmt.Errorf("invalid healthPath")
	}
	if timeout, ok := l["readyTimeoutMs"]; ok {
		n, ok := timeout.(float64)
		if !ok || n <= 0 || n > 120000 || n != float64(int64(n)) {
			return "", nil, fmt.Errorf("invalid readyTimeoutMs")
		}
	}
	args, ok := l["runtimeArgs"].([]any)
	if !ok || len(args) == 0 {
		return "", nil, fmt.Errorf("runtimeArgs required")
	}
	first, ok := args[0].(string)
	if !ok || !strings.HasPrefix(first, "app/") || path.Clean(first) != first || strings.ContainsAny(first, "\\\x00") || !regular[first] {
		return "", nil, fmt.Errorf("first runtime argument must be an existing app/ entry file")
	}
	entry := false
	for _, v := range args {
		arg, ok := v.(string)
		if !ok || strings.Contains(arg, "--port") {
			return "", nil, fmt.Errorf("invalid runtime arg")
		}
		clean := path.Clean(arg)
		if strings.HasPrefix(clean, "app/") && regular[clean] {
			entry = true
		}
	}
	if !entry || !regular["app/app.js"] || !regular["app/public/index.html"] {
		return "", nil, fmt.Errorf("application entry or UI missing")
	}
	var document map[string]any
	if yaml.Unmarshal(doc, &document) != nil {
		return "", nil, fmt.Errorf("invalid OpenAPI")
	}
	version, _ := document["openapi"].(string)
	if !strings.HasPrefix(version, "3.1.") {
		return "", nil, fmt.Errorf("artifact requires OpenAPI 3.1")
	}
	paths, ok := document["paths"].(map[string]any)
	if !ok {
		return "", nil, fmt.Errorf("paths required")
	}
	for p := range paths {
		if !strings.HasPrefix(p, "/api/") {
			return "", nil, fmt.Errorf("business routes must start /api/")
		}
	}
	if raw, exists := document["servers"]; exists {
		servers, ok := raw.([]any)
		if !ok {
			return "", nil, fmt.Errorf("servers must be array")
		}
		for _, v := range servers {
			m, _ := v.(map[string]any)
			raw, _ := m["url"].(string)
			u, err := url.Parse(raw)
			if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
				return "", nil, fmt.Errorf("servers must be relative")
			}
		}
	}
	controller, err := openapi.FromDocument("artifact", "http://127.0.0.1", doc)
	if err != nil {
		return "", nil, err
	}
	parsed, _ := controller.Document("artifact")
	normalized, err := json.Marshal(parsed)
	if err != nil {
		return "", nil, err
	}
	name, _ := l["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Application"
	}
	if len([]rune(name)) > 120 {
		name = string([]rune(name)[:120])
	}
	return name, normalized, nil
}

// Handler serves only unguessable artifact names; the URL is a capability for Render.
func (s Store) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/artifacts/")
		key := strings.TrimSuffix(name, ".tar.gz")
		if len(key) != 48 || name != key+".tar.gz" {
			http.NotFound(w, r)
			return
		}
		if _, err := hex.DecodeString(key); err != nil {
			http.NotFound(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		http.ServeFile(w, r, filepath.Join(s.Dir, name))
	})
}
