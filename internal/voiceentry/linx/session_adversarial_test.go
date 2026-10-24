package linx

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// failingWriteBody completes the WebSocket handshake but refuses to carry any
// frame, which is what a peer that drops the TCP connection right after the
// upgrade looks like to the writer.
type failingWriteBody struct{ input io.Reader }

func (b *failingWriteBody) Read(p []byte) (int, error) { return b.input.Read(p) }
func (b *failingWriteBody) Write([]byte) (int, error) {
	return 0, errors.New("connection reset")
}
func (b *failingWriteBody) Close() error { return nil }

func TestNewSessionReportsAHelloWriteFailure(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		key := request.Header.Get("Sec-WebSocket-Key")
		sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		header := http.Header{}
		header.Set("Connection", "Upgrade")
		header.Set("Upgrade", "websocket")
		header.Set("Sec-WebSocket-Accept", base64.StdEncoding.EncodeToString(sum[:]))
		return &http.Response{
			StatusCode: http.StatusSwitchingProtocols,
			Status:     "101 Switching Protocols",
			Header:     header,
			Body:       &failingWriteBody{input: bytes.NewReader(nil)},
			Request:    request,
		}, nil
	})}
	_, err := NewSession(Config{
		Device:     Device{WSURL: "ws://example.invalid/ws", Token: "t", MAC: "m", ClientID: "c"},
		HTTPClient: client,
	})
	if err == nil {
		t.Fatal("a session was created without a deliverable hello")
	}
}
