package artifact

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Item describes one stored artifact and its download address.
type Item struct {
	AppID       string
	Path        string
	Size        int64
	SHA256      string
	Token       string
	DownloadURL string
	StoredAt    time.Time
}

// Repository persists artifact metadata across restarts.
type Repository interface {
	SaveArtifact(Item) error
	ListArtifacts() ([]Item, error)
}

// Store keeps artifacts on disk and exposes tokenized download URLs for
// Render.
type Store struct {
	dir     string
	baseURL string
	repo    Repository

	mu    sync.Mutex
	items map[string]Item
}

// NewStore prepares the artifact directory and restores persisted metadata.
func NewStore(dir, baseURL string, repo Repository) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("artifact directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	store := &Store{dir: dir, baseURL: strings.TrimRight(baseURL, "/"), repo: repo, items: make(map[string]Item)}
	if repo != nil {
		items, err := repo.ListArtifacts()
		if err != nil {
			return nil, fmt.Errorf("load artifacts: %w", err)
		}
		for _, item := range items {
			if !safeID(item.AppID) {
				continue
			}
			// The configured directory wins over a stale absolute path from a
			// previous deployment.
			item.Path = filepath.Join(store.dir, item.AppID+".tar.gz")
			item.DownloadURL = store.downloadURL(item.AppID, item.Token)
			if err := repo.SaveArtifact(item); err != nil {
				return nil, fmt.Errorf("restore artifact location: %w", err)
			}
			store.items[item.AppID] = item
		}
	}
	return store, nil
}

// SaveFile copies source into the store and records its digest and URL.
func (s *Store) SaveFile(appID, source string, size int64, digest string) (Item, error) {
	if !safeID(appID) {
		return Item{}, fmt.Errorf("invalid app id %q", appID)
	}
	final := filepath.Join(s.dir, appID+".tar.gz")
	temp := final + ".tmp"
	if err := copyFile(source, temp); err != nil {
		return Item{}, err
	}
	actualSize, actualDigest, err := fileDigest(temp)
	if err != nil {
		os.Remove(temp)
		return Item{}, err
	}
	if actualSize != size || !strings.EqualFold(actualDigest, digest) {
		os.Remove(temp)
		return Item{}, fmt.Errorf("artifact changed while storing")
	}
	if err := os.Rename(temp, final); err != nil {
		os.Remove(temp)
		return Item{}, err
	}
	token := rand.Text()
	item := Item{
		AppID:       appID,
		Path:        final,
		Size:        actualSize,
		SHA256:      actualDigest,
		Token:       token,
		DownloadURL: s.downloadURL(appID, token),
		StoredAt:    time.Now().UTC(),
	}
	if s.repo != nil {
		if err := s.repo.SaveArtifact(item); err != nil {
			os.Remove(final)
			return Item{}, fmt.Errorf("persist artifact: %w", err)
		}
	}
	s.mu.Lock()
	s.items[appID] = item
	s.mu.Unlock()
	return item, nil
}

func (s *Store) downloadURL(appID, token string) string {
	return s.baseURL + "/artifacts/" + url.PathEscape(appID) + "/artifact.tar.gz?token=" + url.QueryEscape(token)
}

// Get returns the item stored for appID.
func (s *Store) Get(appID string) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[appID]
	return item, ok
}

// ServeDownload serves GET /artifacts/{app_id}/{name}.
func (s *Store) ServeDownload(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("app_id")
	name := r.PathValue("name")
	item, ok := s.Get(appID)
	if !ok || name != "artifact.tar.gz" {
		http.NotFound(w, r)
		return
	}
	token := r.URL.Query().Get("token")
	if subtle.ConstantTimeCompare([]byte(token), []byte(item.Token)) != 1 {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(item.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="artifact.tar.gz"`)
	http.ServeContent(w, r, "artifact.tar.gz", info.ModTime(), file)
}

func safeID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case char == '_' || char == '-':
		default:
			return false
		}
	}
	return true
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(target)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(target)
		return err
	}
	return nil
}

func fileDigest(filename string) (int64, string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}
