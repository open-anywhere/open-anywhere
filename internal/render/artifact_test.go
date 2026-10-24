package render

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func testTar(t *testing.T, headers []*tar.Header, bodies [][]byte) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for i, h := range headers {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(bodies[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func fixtureTar(t *testing.T) string {
	t.Helper()
	var headers []*tar.Header
	var bodies [][]byte
	root := "../../e2e/testdata/scoreboard"
	if err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		headers = append(headers, &tar.Header{Name: filepath.ToSlash(rel), Mode: 0600, Size: int64(len(b))})
		bodies = append(bodies, b)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return testTar(t, headers, bodies)
}
func TestArtifactContract(t *testing.T) {
	dir := t.TempDir()
	if err := unpack(context.Background(), fixtureTar(t), dir); err != nil {
		t.Fatal(err)
	}
	l, err := validateArtifact(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.RuntimeExecutable != "node" || l.RuntimeArgs[0] != "app/app.js" {
		t.Fatal(l)
	}
	if err = os.WriteFile(filepath.Join(dir, "launch.json"), []byte(`{"name":"bad","runtimeExecutable":"node","runtimeArgs":["app/app.js"],"port":3000}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = validateArtifact(dir); err == nil {
		t.Fatal("fixed port unexpectedly accepted")
	}
}
func TestArchiveRejectsEscapesLinksAndDuplicates(t *testing.T) {
	for _, h := range []*tar.Header{
		{Name: "../outside", Typeflag: tar.TypeReg},
		{Name: "/tmp/outside", Typeflag: tar.TypeReg},
		{Name: "app/link", Typeflag: tar.TypeSymlink, Linkname: "/tmp"},
		{Name: "app/hard", Typeflag: tar.TypeLink, Linkname: "launch.json"},
		{Name: "app/pipe", Typeflag: tar.TypeFifo},
	} {
		t.Run(h.Name, func(t *testing.T) {
			if err := unpack(context.Background(), testTar(t, []*tar.Header{h}, [][]byte{nil}), t.TempDir()); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	h := &tar.Header{Name: "launch.json", Typeflag: tar.TypeReg, Size: 2}
	if err := unpack(context.Background(), testTar(t, []*tar.Header{h, h}, [][]byte{[]byte("{}"), []byte("{}")}), t.TempDir()); err == nil {
		t.Fatal("duplicate entry accepted")
	}
}
func TestArtifactTruncationAndMissingFiles(t *testing.T) {
	p := fixtureTar(t)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p, data[:len(data)-8], 0600); err != nil {
		t.Fatal(err)
	}
	if err = unpack(context.Background(), p, t.TempDir()); err == nil {
		t.Fatal("truncated gzip accepted")
	}
	if _, err = validateArtifact(t.TempDir()); err == nil {
		t.Fatal("empty artifact accepted")
	}
}
func TestOpenAPIRequiresBusinessOperations(t *testing.T) {
	for _, spec := range []string{
		"openapi: 3.0.3\ninfo: {title: test, version: '1'}\npaths: {}",
		"openapi: 3.1.0\ninfo: {title: test, version: '1'}\npaths:\n  /health:\n    get: {operationId: health}",
		"openapi: 3.1.0\ninfo: {title: test, version: '1'}\npaths:\n  /api/x:\n    get: {operationId: same, description: x, responses: {'200': {description: x}}}\n    post: {operationId: same, description: x, responses: {'200': {description: x}}}",
	} {
		if err := validateOpenAPI([]byte(spec)); err == nil {
			t.Fatalf("bad contract accepted: %s", spec)
		}
	}
}

func TestArtifactCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := unpack(ctx, fixtureTar(t), t.TempDir()); err != context.Canceled {
		t.Fatalf("got %v", err)
	}
}

func TestRejectMalformedLaunchAndMetadata(t *testing.T) {
	for _, body := range []string{
		`{"name":"test","runtimeExecutable":"python","runtimeArgs":["app/app.js"]}`,
		`{"name":"test","runtimeExecutable":"node","runtimeArgs":["--eval","process.exit()"]}`,
		`{"name":"test","runtimeExecutable":"node","runtimeArgs":["app/app.js","--port=3000"]}`,
		`{"name":"test","runtimeExecutable":"node","runtimeArgs":["app/app.js"],"healthPath":"/other"}`,
		`{"name":"test","runtimeExecutable":"node","runtimeArgs":["app/app.js"],"readyTimeoutMs":-1}`,
		`{"name":"test","runtimeExecutable":"node","runtimeArgs":["app/app.js"]} {}`,
	} {
		dir := t.TempDir()
		if err := unpack(context.Background(), fixtureTar(t), dir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "launch.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := validateArtifact(dir); err == nil {
			t.Fatalf("bad launch accepted: %s", body)
		}
	}
	dir := t.TempDir()
	if err := unpack(context.Background(), fixtureTar(t), dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app/package.json"), []byte(`null`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateArtifact(dir); err == nil {
		t.Fatal("null package metadata accepted")
	}
}
