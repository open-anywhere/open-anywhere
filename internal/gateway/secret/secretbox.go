// Package secret encrypts platform keys and user service tokens before they
// are stored in SQLite. The master key comes from GATEWAY_SECRET_KEY or a
// generated 0600 file inside the portal data directory.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// EnvKey names the environment variable holding the base64 master key.
	EnvKey = "GATEWAY_SECRET_KEY"
	// KeyFile is the master key file inside the portal data directory.
	KeyFile = "secret.key"
	// prefix marks an encrypted value. It leaves room for future key
	// rotation schemes.
	prefix = "enc:v1:"
	keyLen = 32
)

// ErrDecrypt reports a value that cannot be decrypted with this box. It is
// never recovered from silently: callers surface it and stop.
var ErrDecrypt = errors.New("secret: cannot decrypt value with the configured master key")

// Box seals and opens values with AES-256-GCM.
type Box struct {
	aead cipher.AEAD
}

// New builds a box from a raw 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != keyLen {
		return nil, fmt.Errorf("secret: master key must be %d bytes, got %d", keyLen, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret: create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret: create GCM: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Load reads the master key from the environment, then from dataDir/secret.key.
// When neither exists it generates a key file so a fresh deployment works
// without extra steps. An unreadable or malformed key is an error: generating
// a new one would make existing ciphertext undecryptable.
func Load(dataDir string, getenv func(string) string) (*Box, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if encoded := strings.TrimSpace(getenv(EnvKey)); encoded != "" {
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("secret: %s is not valid base64: %w", EnvKey, err)
		}
		return New(key)
	}
	if dataDir == "" {
		return nil, errors.New("secret: portal data directory is required to load the master key")
	}
	path := filepath.Join(dataDir, KeyFile)
	encoded, err := os.ReadFile(path)
	if err == nil {
		key, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
		if decodeErr != nil {
			return nil, fmt.Errorf("secret: %s is not valid base64: %w", path, decodeErr)
		}
		return New(key)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("secret: read master key: %w", err)
	}
	return generate(dataDir, path)
}

// Available reports whether a master key is configured through the environment
// or the key file, without generating one. Startup uses it to refuse creating a
// fresh key over a database that already holds ciphertext.
func Available(dataDir string, getenv func(string) string) bool {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if strings.TrimSpace(getenv(EnvKey)) != "" {
		return true
	}
	if dataDir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dataDir, KeyFile))
	return err == nil
}

func generate(dataDir, path string) (*Box, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("secret: create portal data directory: %w", err)
	}
	key := make([]byte, keyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("secret: generate master key: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	temp, err := os.CreateTemp(dataDir, ".secret-*.key")
	if err != nil {
		return nil, fmt.Errorf("secret: create master key file: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return nil, fmt.Errorf("secret: secure master key file: %w", err)
	}
	if _, err := temp.WriteString(encoded + "\n"); err != nil {
		temp.Close()
		return nil, fmt.Errorf("secret: write master key file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return nil, fmt.Errorf("secret: close master key file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return nil, fmt.Errorf("secret: install master key file: %w", err)
	}
	return New(key)
}

// IsEncrypted reports whether value already carries the envelope prefix.
func IsEncrypted(value string) bool { return strings.HasPrefix(value, prefix) }

// Encrypt seals plaintext. The additional data binds the ciphertext to a
// context such as platform:<key> or binding:<user>:render, so a value cannot
// be moved between fields.
func (b *Box) Encrypt(plaintext, aad string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secret: generate nonce: %w", err)
	}
	sealed := b.aead.Seal(nil, nonce, []byte(plaintext), []byte(aad))
	payload := append(nonce, sealed...)
	return prefix + base64.StdEncoding.EncodeToString(payload), nil
}

// Decrypt opens a value produced by Encrypt.
func (b *Box) Decrypt(value, aad string) (string, error) {
	if !IsEncrypted(value) {
		return "", ErrDecrypt
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil {
		return "", ErrDecrypt
	}
	if len(payload) < b.aead.NonceSize() {
		return "", ErrDecrypt
	}
	nonce := payload[:b.aead.NonceSize()]
	ciphertext := payload[b.aead.NonceSize():]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, []byte(aad))
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plaintext), nil
}

// AAD helpers keep the binding format in one place.
func PlatformAAD(key string) string { return "platform:" + key }

func BindingAAD(userID, service string) string { return "binding:" + userID + ":" + service }
