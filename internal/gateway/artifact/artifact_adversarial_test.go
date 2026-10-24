package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	if _, err := gz.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func writeBytes(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func writeHugeArchive(t *testing.T, size int64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	writer := tar.NewWriter(gz)
	header := &tar.Header{Name: "app/huge.bin", Mode: 0o644, Size: size, Typeflag: tar.TypeReg}
	if err := writer.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(writer, zeroReader{}, size); err != nil {
		t.Fatal(err)
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

func TestValidateSurfacesFileErrors(t *testing.T) {
	// A directory can be stat'ed but cannot be hashed, so Validate must return
	// the digest error rather than treating the size as an artifact.
	if _, err := Validate(t.TempDir()); err == nil {
		t.Fatal("Validate accepted a directory")
	}
	if _, err := fileSHA256(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("fileSHA256 accepted a missing file")
	}
}

func TestValidateArchiveReportsAnOpenFailure(t *testing.T) {
	if _, err := validateArchive(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("validateArchive accepted a missing file")
	}
}

func TestValidateRejectsCorruptGzipAndTar(t *testing.T) {
	for name, data := range map[string][]byte{
		"not gzip":        gzipBytes(t, []byte("this is not a tar archive")),
		"not gzip at all": []byte("plain text"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Validate(writeBytes(t, data)); err == nil || !IsCode(err, "ARTIFACT_CORRUPT") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestValidateIgnoresTheArchiveRootEntry(t *testing.T) {
	entries := append([]entry{{name: "."}}, validEntries()...)
	if _, err := Validate(writeArchive(t, entries)); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsAnEntryLargerThanTheFileLimit(t *testing.T) {
	_, err := Validate(writeHugeArchive(t, maxFileSize+1))
	if err == nil || !IsCode(err, "ARTIFACT_TOO_LARGE") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateRejectsAnOversizedLaunchFile(t *testing.T) {
	entries := validEntries()
	entries[4].body = strings.Repeat("a", (1<<20)+1)
	_, err := Validate(writeArchive(t, entries))
	if err == nil || !IsCode(err, "INVALID_ARTIFACT") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateDetectsATruncatedGzipTrailer(t *testing.T) {
	data, err := os.ReadFile(writeArchive(t, validEntries()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(writeBytes(t, data[:len(data)-4])); err == nil {
		t.Fatal("a truncated gzip stream was accepted")
	}
}

func TestValidateRequiresAtLeastOneAppFile(t *testing.T) {
	entries := validEntries()
	for _, name := range []string{"app/", "app/app.js", "app/package.json", "app/public/index.html"} {
		entries = remove(entries, name)
	}
	_, err := Validate(writeArchive(t, entries))
	if err == nil || !strings.Contains(err.Error(), "app/") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseLaunchRequiresAName(t *testing.T) {
	entries := validEntries()
	entries[4].body = `{"runtimeExecutable":"node","runtimeArgs":["app/app.js"]}`
	_, err := Validate(writeArchive(t, entries))
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseLaunchIgnoresArgumentsThatCannotBeNormalized(t *testing.T) {
	entries := validEntries()
	entries[4].body = `{"name":"quiz","runtimeExecutable":"node","runtimeArgs":["app/app.js","../evil"]}`
	if _, err := Validate(writeArchive(t, entries)); err != nil {
		t.Fatalf("an argument with a traversal segment should only be ignored: %v", err)
	}
}

func TestReadEntryPropagatesATruncatedBody(t *testing.T) {
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gz)
	header := &tar.Header{Name: "launch.json", Mode: 0o644, Size: 100, Typeflag: tar.TypeReg}
	if err := writer.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("short")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(writeBytes(t, buffer.Bytes())); err == nil || !IsCode(err, "ARTIFACT_CORRUPT") {
		t.Fatalf("err = %v", err)
	}
}

func TestSaveFileRemovesTheTempCopyWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir, "http://gateway", nil)
	if err != nil {
		t.Fatal(err)
	}
	source := writeBytes(t, []byte("artifact"))
	if err := os.MkdirAll(filepath.Join(dir, "app_1.tar.gz"), 0o755); err != nil {
		t.Fatal(err)
	}
	digest, size, err := digestAndSize(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveFile("app_1", source, size, digest); err == nil {
		t.Fatal("SaveFile overwrote an existing directory")
	}
	if _, err := os.Stat(filepath.Join(dir, "app_1.tar.gz.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temp copy was left behind: %v", err)
	}
}

func digestAndSize(path string) (string, int64, error) {
	size, digest, err := fileDigest(path)
	return digest, size, err
}

func TestRemoveReportsADirectoryThatCannotBeDeleted(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir, "http://gateway", nil)
	if err != nil {
		t.Fatal(err)
	}
	source := writeArchive(t, validEntries())
	digest, size, err := digestAndSize(source)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.SaveFile("app_1", source, size, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(item.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(item.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(item.Path, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("app_1"); err == nil {
		t.Fatal("Remove reported success for a non-empty directory")
	}
	if _, ok := store.Get("app_1"); ok {
		t.Fatal("the item was not forgotten")
	}
}

func TestRemoveWithoutARepository(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir, "http://gateway", nil)
	if err != nil {
		t.Fatal(err)
	}
	source := writeArchive(t, validEntries())
	digest, size, err := digestAndSize(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveFile("app_1", source, size, digest); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("app_1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

func TestCopyAndDigestReportReadFailures(t *testing.T) {
	dir := t.TempDir()
	if err := copyFile(dir, filepath.Join(t.TempDir(), "target")); err == nil {
		t.Fatal("copyFile read a directory")
	}
	if _, _, err := fileDigest(dir); err == nil {
		t.Fatal("fileDigest read a directory")
	}
}

func TestValidateRejectsAnArchiveThatExpandsBeyondTheLimit(t *testing.T) {
	var buffer bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buffer, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(gz)
	for _, item := range validEntries() {
		header := &tar.Header{Name: item.name, Mode: 0o644, Size: int64(len(item.body)), Typeflag: item.typeflag}
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
	if _, err := io.CopyN(gz, zeroReader{}, maxTotal+1); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Validate(writeBytes(t, buffer.Bytes()))
	if err == nil || !strings.Contains(err.Error(), "expanded archive too large") {
		t.Fatalf("err = %v", err)
	}
}
