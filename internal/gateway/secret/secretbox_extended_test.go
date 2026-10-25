package secret

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAvailableDetectsConfiguredKeys(t *testing.T) {
	dir := t.TempDir()
	if Available(dir, nil) {
		t.Fatal("a directory without a key file was reported as available")
	}
	if Available("", nil) {
		t.Fatal("an empty data directory was reported as available")
	}
	if Available("", func(string) string { return "  " }) {
		t.Fatal("a blank environment key was reported as available")
	}
	if !Available("", func(name string) string {
		if name == EnvKey {
			return "c2VjcmV0"
		}
		return ""
	}) {
		t.Fatal("an environment key was not detected")
	}
	if err := os.WriteFile(filepath.Join(dir, KeyFile), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !Available(dir, nil) {
		t.Fatal("a key file was not detected")
	}
}

func TestLoadRequiresAKeySource(t *testing.T) {
	if _, err := Load("", nil); err == nil || !strings.Contains(err.Error(), "data directory is required") {
		t.Fatalf("err = %v", err)
	}
	// A regular file cannot contain the key file, so reading fails with a real
	// I/O error rather than being treated as a missing key.
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(dir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, nil); err == nil || !strings.Contains(err.Error(), "read master key") {
		t.Fatalf("err = %v", err)
	}
}

func TestGenerateReportsFilesystemFailures(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := generate(blocker, filepath.Join(blocker, KeyFile)); err == nil || !strings.Contains(err.Error(), "create portal data directory") {
		t.Fatalf("mkdir err = %v", err)
	}

	// Renaming over an existing directory must fail instead of replacing it.
	target := filepath.Join(dir, KeyFile)
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := generate(dir, target); err == nil || !strings.Contains(err.Error(), "install master key file") {
		t.Fatalf("rename err = %v", err)
	}
}

func TestGenerateReportsAnUnwritableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not apply on Windows")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, err := generate(dir, filepath.Join(dir, KeyFile)); err == nil {
		// Running as root ignores the mode bits; there is no portable way to
		// make CreateTemp fail then.
		t.Skip("the process can still write to a 0500 directory")
	} else if !strings.Contains(err.Error(), "create master key file") {
		t.Fatalf("err = %v", err)
	}
}

func TestDecryptRejectsMalformedCiphertexts(t *testing.T) {
	box, err := New(testKey())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		prefix + "!!!not-base64!!!",
		prefix + base64.StdEncoding.EncodeToString([]byte{1, 2, 3}),
	} {
		if _, err := box.Decrypt(value, "aad"); err != ErrDecrypt {
			t.Fatalf("Decrypt(%q) = %v, want ErrDecrypt", value, err)
		}
	}
}

func TestBindingAAD(t *testing.T) {
	if got := BindingAAD("usr_1", "render"); got != "binding:usr_1:render" {
		t.Fatalf("BindingAAD = %q", got)
	}
}
