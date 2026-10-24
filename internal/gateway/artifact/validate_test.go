package artifact

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validOpenAPI = `openapi: 3.1.0
paths:
  /api/roll:
    post:
      operationId: rollDice
      responses:
        "200":
          description: ok
`

type entry struct {
	name     string
	body     string
	typeflag byte
	linkname string
}

func validEntries() []entry {
	return []entry{
		{name: "app/", typeflag: tar.TypeDir},
		{name: "app/app.js", body: "console.log('quiz')"},
		{name: "app/package.json", body: `{"name":"quiz","private":true}`},
		{name: "app/public/index.html", body: "<html></html>"},
		{name: "launch.json", body: `{"name":"quiz","runtimeExecutable":"node","runtimeArgs":["app/app.js"]}`},
		{name: "openapi.yaml", body: validOpenAPI},
	}
}

func writeArchive(t *testing.T, entries []entry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	writer := tar.NewWriter(gz)
	for _, item := range entries {
		header := &tar.Header{Name: item.name, Mode: 0o644, Size: int64(len(item.body)), Typeflag: item.typeflag, Linkname: item.linkname}
		if item.typeflag == tar.TypeDir {
			header.Mode = 0o755
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if item.typeflag == tar.TypeReg || item.typeflag == 0 {
			if _, err := writer.Write([]byte(item.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidateAcceptsContract(t *testing.T) {
	result, err := Validate(writeArchive(t, validEntries()))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Launch.Name != "quiz" || result.Launch.HealthPath != "/health" || result.Launch.ReadyTimeoutMS != 10000 {
		t.Fatalf("launch = %+v", result.Launch)
	}
	if result.Launch.RuntimeExecutable != "node" || len(result.Launch.RuntimeArgs) != 1 {
		t.Fatalf("runtime = %+v", result.Launch)
	}
	if _, ok := result.OpenAPI.Lookup("rollDice"); !ok {
		t.Fatal("OpenAPI operation not indexed")
	}
	if result.Size <= 0 || len(result.SHA256) != 64 {
		t.Fatalf("size=%d sha=%q", result.Size, result.SHA256)
	}
}

func TestValidateHonorsReadyTimeout(t *testing.T) {
	entries := validEntries()
	entries[4].body = `{"name":"quiz","runtimeExecutable":"node","runtimeArgs":["app/app.js"],"healthPath":"/health","readyTimeoutMs":5000}`
	result, err := Validate(writeArchive(t, entries))
	if err != nil {
		t.Fatal(err)
	}
	if result.Launch.ReadyTimeoutMS != 5000 {
		t.Fatalf("ready = %d", result.Launch.ReadyTimeoutMS)
	}
}

func TestValidateRejectsInvalidArchives(t *testing.T) {
	tests := map[string]func([]entry) []entry{
		"missing launch":  func(e []entry) []entry { return remove(e, "launch.json") },
		"missing openapi": func(e []entry) []entry { return remove(e, "openapi.yaml") },
		"no app files":    func(e []entry) []entry { return remove(remove(e, "app/app.js"), "app/package.json") },
		"outputs nesting": func(e []entry) []entry { return append(e, entry{name: "outputs/launch.json"}) },
		"traversal":       func(e []entry) []entry { return append(e, entry{name: "../evil"}) },
		"absolute":        func(e []entry) []entry { return append(e, entry{name: "/evil"}) },
		"symlink": func(e []entry) []entry {
			return append(e, entry{name: "link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"})
		},
		"port field": func(e []entry) []entry {
			e[4].body = `{"name":"quiz","runtimeExecutable":"node","runtimeArgs":["app/app.js"],"port":8080}`
			return e
		},
		"wrong runtime": func(e []entry) []entry {
			e[4].body = `{"name":"quiz","runtimeExecutable":"python","runtimeArgs":["app/app.js"]}`
			return e
		},
		"no args": func(e []entry) []entry {
			e[4].body = `{"name":"quiz","runtimeExecutable":"node"}`
			return e
		},
		"entry outside app": func(e []entry) []entry {
			e[4].body = `{"name":"quiz","runtimeExecutable":"node","runtimeArgs":["openapi.yaml"]}`
			return e
		},
		"fixed port arg": func(e []entry) []entry {
			e[4].body = `{"name":"quiz","runtimeExecutable":"node","runtimeArgs":["app/app.js","--port=8080"]}`
			return e
		},
		"wrong health path": func(e []entry) []entry {
			e[4].body = `{"name":"quiz","runtimeExecutable":"node","runtimeArgs":["app/app.js"],"healthPath":"/ready"}`
			return e
		},
		"zero ready timeout": func(e []entry) []entry {
			e[4].body = `{"name":"quiz","runtimeExecutable":"node","runtimeArgs":["app/app.js"],"readyTimeoutMs":0}`
			return e
		},
		"excessive ready timeout": func(e []entry) []entry {
			e[4].body = `{"name":"quiz","runtimeExecutable":"node","runtimeArgs":["app/app.js"],"readyTimeoutMs":600001}`
			return e
		},
		"unknown launch field": func(e []entry) []entry {
			e[4].body = `{"name":"quiz","runtimeExecutable":"node","runtimeArgs":["app/app.js"],"extra":true}`
			return e
		},
		"trailing json": func(e []entry) []entry {
			e[4].body = `{"name":"quiz","runtimeExecutable":"node","runtimeArgs":["app/app.js"]} {}`
			return e
		},
		"invalid openapi": func(e []entry) []entry {
			e[5].body = "openapi: 3.0.0\n"
			return e
		},
		"invalid package json": func(e []entry) []entry {
			e[2].body = "{"
			return e
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			entries := mutate(validEntries())
			_, err := Validate(writeArchive(t, entries))
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !IsCode(err, "INVALID_ARTIFACT") {
				t.Fatalf("code = %v", err)
			}
		})
	}
}

func TestValidateRejectsTooLargeAndCorrupt(t *testing.T) {
	large := filepath.Join(t.TempDir(), "large.tar.gz")
	file, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxSize + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := Validate(large); !IsCode(err, "ARTIFACT_TOO_LARGE") {
		t.Fatalf("err = %v", err)
	}

	corrupt := filepath.Join(t.TempDir(), "corrupt.tar.gz")
	if err := os.WriteFile(corrupt, []byte("not gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(corrupt); !IsCode(err, "ARTIFACT_CORRUPT") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateRejectsUnreadableFile(t *testing.T) {
	if _, err := Validate(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected error")
	}
}

func TestCleanName(t *testing.T) {
	tests := map[string]string{
		"app/app.js":    "app/app.js",
		"./app/app.js":  "app/app.js",
		".":             "",
		"app/../app.js": "",
	}
	for input, want := range tests {
		got, err := cleanName(input)
		if want == "" && input != "." {
			if err == nil {
				t.Fatalf("cleanName(%q) should fail", input)
			}
			continue
		}
		if err != nil || got != want {
			t.Fatalf("cleanName(%q) = %q, %v", input, got, err)
		}
	}
	if _, err := cleanName("../x"); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("err = %v", err)
	}
	if _, err := cleanName("/abs"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("err = %v", err)
	}
}

func remove(entries []entry, name string) []entry {
	result := entries[:0]
	for _, item := range entries {
		if item.name != name {
			result = append(result, item)
		}
	}
	return result
}
