package render

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const maxArtifactBytes = 100 << 20

type launch struct {
	Name              string   `json:"name"`
	RuntimeExecutable string   `json:"runtimeExecutable"`
	RuntimeArgs       []string `json:"runtimeArgs"`
	HealthPath        string   `json:"healthPath"`
	ReadyTimeoutMS    int      `json:"readyTimeoutMs"`
}

func download(ctx context.Context, client *http.Client, url, dir string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("artifact download returned HTTP %d", res.StatusCode)
	}
	f, err := os.Create(filepath.Join(dir, "artifact.tar.gz"))
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(res.Body, maxArtifactBytes+1))
	if err != nil {
		return err
	}
	if n > maxArtifactBytes {
		return errors.New("compressed artifact exceeds 100 MiB")
	}
	return nil
}

func unpack(ctx context.Context, archive, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var size int64
	seen := map[string]bool{}
	for count := 0; ; count++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if count >= 4096 {
			return errors.New("too many artifact entries")
		}
		if strings.Contains(h.Name, "\\") || path.IsAbs(h.Name) {
			return errors.New("invalid archive path")
		}
		for _, part := range strings.Split(h.Name, "/") {
			if part == ".." {
				return errors.New("archive path escapes root")
			}
		}
		name := path.Clean(h.Name)
		if name == "." && h.Typeflag == tar.TypeDir {
			continue
		}
		if name == "." || seen[name] {
			return errors.New("duplicate or invalid archive entry")
		}
		seen[name] = true
		full := filepath.Join(dst, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(full, 0700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			size += h.Size
			if h.Size < 0 || size > maxArtifactBytes {
				return errors.New("expanded artifact exceeds 100 MiB")
			}
			if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
				return err
			}
			out, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(out, tr, h.Size)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return errors.New("artifact links and special files are not supported")
		}
	}
	// Consume the gzip trailer, including its checksum.
	n, err := io.Copy(io.Discard, io.LimitReader(gz, maxArtifactBytes+1))
	if n > maxArtifactBytes {
		return errors.New("excess trailing archive data")
	}
	return err
}

func validateArtifact(dir string) (launch, error) {
	var l launch
	for _, name := range []string{"launch.json", "openapi.yaml", "app/app.js", "app/package.json", "app/public/index.html"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() {
			return l, fmt.Errorf("required regular file missing: %s", name)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "launch.json"))
	if err != nil {
		return l, err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&l); err != nil {
		return l, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return l, errors.New("launch.json must contain one JSON object")
	}
	if strings.TrimSpace(l.Name) == "" || l.RuntimeExecutable != "node" {
		return l, errors.New("name is required and runtimeExecutable must be node")
	}
	if len(l.RuntimeArgs) == 0 || l.RuntimeArgs[0] != "app/app.js" {
		return l, errors.New("runtimeArgs must start with app/app.js")
	}
	for _, arg := range l.RuntimeArgs[1:] {
		if arg == "--port" || strings.HasPrefix(arg, "--port=") {
			return l, errors.New("port must be injected through PORT")
		}
	}
	if l.HealthPath == "" {
		l.HealthPath = "/health"
	}
	if l.HealthPath != "/health" {
		return l, errors.New("healthPath must be /health")
	}
	if l.ReadyTimeoutMS == 0 {
		l.ReadyTimeoutMS = 10000
	}
	if l.ReadyTimeoutMS < 0 || l.ReadyTimeoutMS > int(time.Minute/time.Millisecond) {
		return l, errors.New("readyTimeoutMs must be between 1 and 60000")
	}
	data, err = os.ReadFile(filepath.Join(dir, "app/package.json"))
	if err != nil {
		return l, err
	}
	var metadata map[string]any
	if json.Unmarshal(data, &metadata) != nil || metadata == nil {
		return l, errors.New("invalid package.json")
	}
	data, err = os.ReadFile(filepath.Join(dir, "openapi.yaml"))
	if err != nil {
		return l, err
	}
	if err := validateOpenAPI(data); err != nil {
		return l, err
	}
	return l, nil
}

// validateOpenAPI checks the runtime contract, not every JSON Schema keyword.
// The builder remains responsible for full OpenAPI linting and behavioral tests.
func validateOpenAPI(data []byte) error {
	var spec struct {
		OpenAPI string `yaml:"openapi"`
		Info    struct {
			Title   string `yaml:"title"`
			Version string `yaml:"version"`
		} `yaml:"info"`
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return err
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.1.") || spec.Info.Title == "" || spec.Info.Version == "" || len(spec.Paths) == 0 {
		return errors.New("OpenAPI 3.1 info and business paths are required")
	}
	ids := map[string]bool{}
	for route, item := range spec.Paths {
		if !strings.HasPrefix(route, "/api/") {
			return errors.New("OpenAPI paths must be under /api/")
		}
		for method, node := range item {
			switch method {
			case "get", "post", "put", "patch", "delete", "head", "options", "trace":
			default:
				continue
			}
			var op struct {
				ID          string         `yaml:"operationId"`
				Description string         `yaml:"description"`
				Responses   map[string]any `yaml:"responses"`
			}
			if err := node.Decode(&op); err != nil {
				return err
			}
			if op.ID == "" || ids[op.ID] || op.Description == "" || len(op.Responses) == 0 {
				return errors.New("operations require unique operationId, description and responses")
			}
			ids[op.ID] = true
		}
	}
	if len(ids) == 0 {
		return errors.New("OpenAPI has no business operations")
	}
	return nil
}
