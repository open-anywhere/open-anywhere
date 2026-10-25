package secret

import (
	"runtime"
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testKey() []byte { return bytes.Repeat([]byte{7}, keyLen) }

func TestRoundTripBindsContext(t *testing.T) {
	box, err := New(testKey())
	if err != nil {
		t.Fatal(err)
	}
	first, err := box.Encrypt("hello", PlatformAAD("linx.api_key"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := box.Encrypt("hello", PlatformAAD("linx.api_key"))
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(first) || !strings.HasPrefix(first, "enc:v1:") {
		t.Fatalf("ciphertext = %q", first)
	}
	if first == second {
		t.Fatal("two encryptions produced the same ciphertext")
	}
	plaintext, err := box.Decrypt(first, PlatformAAD("linx.api_key"))
	if err != nil || plaintext != "hello" {
		t.Fatalf("plaintext=%q err=%v", plaintext, err)
	}
	if _, err := box.Decrypt(first, PlatformAAD("agentapi.api_key")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("a value moved between contexts decrypted: %v", err)
	}
	if _, err := box.Decrypt("plaintext", "aad"); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("plaintext was accepted as ciphertext: %v", err)
	}
	other, err := New(bytes.Repeat([]byte{8}, keyLen))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Decrypt(first, PlatformAAD("linx.api_key")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("a different key decrypted the value: %v", err)
	}
}

func TestNewRejectsWrongKeyLength(t *testing.T) {
	if _, err := New([]byte("short")); err == nil {
		t.Fatal("a short key was accepted")
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(testKey())
	box, err := Load(t.TempDir(), func(name string) string {
		if name == EnvKey {
			return key
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if box == nil {
		t.Fatal("no box")
	}
	if _, err := Load("", func(string) string { return "not-base64!" }); err == nil {
		t.Fatal("an invalid environment key was accepted")
	}
}

func TestLoadGeneratesAndReusesKeyFile(t *testing.T) {
	dir := t.TempDir()
	box, err := Load(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := box.Encrypt("value", "aad")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, KeyFile))
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	// Windows has no Unix permission bits; Go always reports 0666 there.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %o", info.Mode().Perm())
	}
	again, err := Load(dir, nil)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if plaintext, err := again.Decrypt(ciphertext, "aad"); err != nil || plaintext != "value" {
		t.Fatalf("plaintext=%q err=%v", plaintext, err)
	}
	if err := os.WriteFile(filepath.Join(dir, KeyFile), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, nil); err == nil {
		t.Fatal("a malformed key file was silently replaced")
	}
}
