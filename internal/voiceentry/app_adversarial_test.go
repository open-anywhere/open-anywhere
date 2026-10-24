package voiceentry

import (
	"errors"
	"log"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type brokenListener struct{ err error }

func (l brokenListener) Accept() (net.Conn, error) { return nil, l.err }
func (brokenListener) Close() error                { return nil }
func (brokenListener) Addr() net.Addr              { return brokenAddr{} }

type brokenAddr struct{}

func (brokenAddr) Network() string { return "broken" }
func (brokenAddr) String() string  { return "broken" }

type syncBuffer struct {
	mu   sync.Mutex
	data strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}

func TestServeHTTPLogsAnUnexpectedServeFailure(t *testing.T) {
	app, err := New(Config{
		Serve:      true,
		DevicePath: filepath.Join(t.TempDir(), "device.json"),
		Listener:   brokenListener{err: errors.New("accept failed")},
	})
	if err != nil {
		t.Fatal(err)
	}
	var output syncBuffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)

	shutdown, err := app.serveHTTP()
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown()

	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(output.String(), "control API:") {
		if time.Now().After(deadline) {
			t.Fatalf("serve failure was not logged: %q", output.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
