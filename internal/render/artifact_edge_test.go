package render

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingReader struct {
	data []byte
	err  error
	done bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.done = true
	return n, nil
}

func TestAdversarialDownloadFilesystemAndCopyErrors(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("payload"))}, nil
	})}
	if err := download(context.Background(), client, "http://example/artifact", filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected create failure")
	}

	client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(&failingReader{data: []byte("x"), err: errors.New("body broken")})}, nil
	})
	if err := download(context.Background(), client, "http://example/artifact", t.TempDir()); err == nil {
		t.Fatal("expected copy failure")
	}
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func unpackBytes(t *testing.T, data []byte, dst string) error {
	t.Helper()
	p := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	return unpack(context.Background(), p, dst)
}

func TestAdversarialUnpackCorruptArchive(t *testing.T) {
	if err := unpackBytes(t, gzipBytes(t, []byte("this is not a tar stream at all........")), t.TempDir()); err == nil {
		t.Fatal("corrupt tar accepted")
	}
}

func TestAdversarialUnpackTooManyEntries(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for i := 0; i < 4097; i++ {
		h := &tar.Header{Name: fmt.Sprintf("f%04d", i), Mode: 0600, Size: 0, Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unpackBytes(t, buf.Bytes(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("expected entry limit, got %v", err)
	}
}

func TestAdversarialUnpackRootEntries(t *testing.T) {
	rootDir := &tar.Header{Name: ".", Typeflag: tar.TypeDir, Mode: 0700}
	if err := unpack(context.Background(), testTar(t, []*tar.Header{rootDir}, [][]byte{nil}), t.TempDir()); err != nil {
		t.Fatalf("root directory entry should be skipped: %v", err)
	}
	rootFile := &tar.Header{Name: ".", Typeflag: tar.TypeReg, Mode: 0600, Size: 0}
	if err := unpack(context.Background(), testTar(t, []*tar.Header{rootFile}, [][]byte{nil}), t.TempDir()); err == nil {
		t.Fatal("root regular file accepted")
	}
}

func TestAdversarialUnpackDirectoryAndFileConflicts(t *testing.T) {
	file := &tar.Header{Name: "a", Typeflag: tar.TypeReg, Mode: 0600, Size: 0}
	dirUnderFile := &tar.Header{Name: "a/b", Typeflag: tar.TypeDir, Mode: 0700}
	if err := unpack(context.Background(), testTar(t, []*tar.Header{file, dirUnderFile}, [][]byte{nil, nil}), t.TempDir()); err == nil {
		t.Fatal("directory below a file accepted")
	}

	fileAgain := &tar.Header{Name: "a", Typeflag: tar.TypeReg, Mode: 0600, Size: 0}
	regUnderFile := &tar.Header{Name: "a/b", Typeflag: tar.TypeReg, Mode: 0600, Size: 0}
	if err := unpack(context.Background(), testTar(t, []*tar.Header{fileAgain, regUnderFile}, [][]byte{nil, nil}), t.TempDir()); err == nil {
		t.Fatal("file below a file accepted")
	}

	dst := t.TempDir()
	writeFile(t, filepath.Join(dst, "a"), "already here", 0600)
	existing := &tar.Header{Name: "a", Typeflag: tar.TypeReg, Mode: 0600, Size: 0}
	if err := unpack(context.Background(), testTar(t, []*tar.Header{existing}, [][]byte{nil}), dst); err == nil {
		t.Fatal("existing destination file was overwritten")
	}
}

// rawTarHeader builds a tar header block without going through tar.Writer so
// tests can express sizes the writer would refuse.
func rawTarHeader(name string, size int64) []byte {
	h := make([]byte, 512)
	copy(h[0:100], name)
	copy(h[100:108], "0000644\x00")
	copy(h[108:116], "0000000\x00")
	copy(h[116:124], "0000000\x00")
	copy(h[124:136], fmt.Sprintf("%011o\x00", size))
	copy(h[136:148], "00000000000\x00")
	for i := 148; i < 156; i++ {
		h[i] = ' '
	}
	h[156] = '0'
	copy(h[257:263], "ustar\x00")
	copy(h[263:265], "00")
	sum := 0
	for _, b := range h {
		sum += int(b)
	}
	copy(h[148:156], fmt.Sprintf("%06o\x00 ", sum))
	return h
}

func TestAdversarialUnpackExpandedSizeLimit(t *testing.T) {
	raw := append(rawTarHeader("small", 0), rawTarHeader("huge", maxArtifactBytes+1)...)
	raw = append(raw, make([]byte, 1024)...)
	if err := unpackBytes(t, gzipBytes(t, raw), t.TempDir()); err == nil || !strings.Contains(err.Error(), "expanded artifact") {
		t.Fatalf("expected expanded size limit, got %v", err)
	}
}

func TestAdversarialUnpackTruncatedBody(t *testing.T) {
	// The header promises 10 bytes but the archive ends immediately.
	raw := rawTarHeader("short", 10)
	if err := unpackBytes(t, gzipBytes(t, raw), t.TempDir()); err == nil {
		t.Fatal("truncated entry body accepted")
	}
}

func TestAdversarialUnpackExcessTrailingData(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	h := &tar.Header{Name: "f", Mode: 0600, Size: 1, Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	chunk := make([]byte, 1<<20)
	for written := int64(0); written <= maxArtifactBytes; written += int64(len(chunk)) {
		if _, err := gz.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unpackBytes(t, buf.Bytes(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("expected trailing data rejection, got %v", err)
	}
}

func TestAdversarialValidateArtifactReadFailures(t *testing.T) {
	for _, name := range []string{"launch.json", "app/package.json", "openapi.yaml"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := unpack(context.Background(), fixtureTar(t), dir); err != nil {
				t.Fatal(err)
			}
			mustChmod(t, filepath.Join(dir, name), 0000)
			if _, err := validateArtifact(dir); err == nil {
				t.Fatalf("unreadable %s accepted", name)
			}
		})
	}
}

func TestAdversarialValidateArtifactDefaults(t *testing.T) {
	launch := `{"name":"defaults","runtimeExecutable":"node","runtimeArgs":["app/app.js"]}`
	dir := t.TempDir()
	if err := unpackBytes(t, standardArtifact(t, launch), dir); err != nil {
		t.Fatal(err)
	}
	l, err := validateArtifact(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.HealthPath != "/health" || l.ReadyTimeoutMS != 10000 {
		t.Fatalf("defaults not applied: %+v", l)
	}
}

func TestAdversarialOpenAPIMalformedYAML(t *testing.T) {
	if err := validateOpenAPI([]byte("openapi: [")); err == nil {
		t.Fatal("malformed YAML accepted")
	}
}

func TestAdversarialOpenAPIUnknownMethodsAndDecodeErrors(t *testing.T) {
	ok := `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /api/x:
    parameters: []
    get: {operationId: a, description: d, responses: {'200': {description: ok}}}
`
	if err := validateOpenAPI([]byte(ok)); err != nil {
		t.Fatalf("path-level parameters should be ignored: %v", err)
	}

	decodeError := `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /api/x:
    get: "broken"
`
	if err := validateOpenAPI([]byte(decodeError)); err == nil {
		t.Fatal("non-mapping operation accepted")
	}

	noOperations := `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /api/x:
    parameters: []
`
	if err := validateOpenAPI([]byte(noOperations)); err == nil {
		t.Fatal("openapi without operations accepted")
	}
}

func TestAdversarialValidateArtifactOpenAPIFailure(t *testing.T) {
	dir := t.TempDir()
	if err := unpackBytes(t, standardArtifact(t, ""), dir); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "openapi.yaml"), "openapi: 3.0.0\n", 0600)
	if _, err := validateArtifact(dir); err == nil {
		t.Fatal("invalid openapi accepted")
	}
}

func TestAdversarialDownloadUsesServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("artifact"))
	}))
	defer server.Close()
	dir := t.TempDir()
	if err := download(context.Background(), server.Client(), server.URL, dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "artifact.tar.gz"))
	if err != nil || string(b) != "artifact" {
		t.Fatalf("unexpected download: %q %v", b, err)
	}
}
