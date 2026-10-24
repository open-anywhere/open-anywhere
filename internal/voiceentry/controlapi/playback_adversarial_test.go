package controlapi

import (
	"context"
	"errors"
	"testing"

	"github.com/coder/websocket"
)

// scriptedPlaybackConn fails the nth write so the hello and packet paths can be
// exercised without a real browser socket.
type scriptedPlaybackConn struct {
	ctx    context.Context
	writes int
	failAt int
	types  []websocket.MessageType
	data   [][]byte
}

func (c *scriptedPlaybackConn) CloseRead(ctx context.Context) context.Context {
	c.ctx = ctx
	return ctx
}

func (c *scriptedPlaybackConn) Write(ctx context.Context, typ websocket.MessageType, data []byte) error {
	c.writes++
	if c.writes == c.failAt {
		return errors.New("write failed")
	}
	c.types = append(c.types, typ)
	c.data = append(c.data, append([]byte(nil), data...))
	return nil
}

func TestStreamPlaybackStopsWhenTheHelloCannotBeWritten(t *testing.T) {
	conn := &scriptedPlaybackConn{failAt: 1}
	server := &Server{}
	server.streamPlayback(context.Background(), conn, make(chan []byte), 48000)
	if conn.writes != 1 {
		t.Fatalf("writes = %d", conn.writes)
	}
	if conn.ctx == nil {
		t.Fatal("CloseRead was not called with the request context")
	}
}

func TestStreamPlaybackStopsWhenAPacketCannotBeWritten(t *testing.T) {
	conn := &scriptedPlaybackConn{failAt: 2}
	packets := make(chan []byte, 1)
	packets <- []byte("opus")
	server := &Server{}
	done := make(chan struct{})
	go func() {
		server.streamPlayback(context.Background(), conn, packets, 16000)
		close(done)
	}()
	<-done
	if conn.writes != 2 {
		t.Fatalf("writes = %d", conn.writes)
	}
	if len(conn.types) != 1 || conn.types[0] != websocket.MessageText {
		t.Fatalf("types = %v", conn.types)
	}
	if string(conn.data[0]) != `{"sample_rate":16000}` {
		t.Fatalf("hello = %q", conn.data[0])
	}
}
