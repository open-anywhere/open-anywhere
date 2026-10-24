package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestRunValidation(t *testing.T) {
	for _, args := range [][]string{
		{}, {"-no-such-flag"}, {"positional"}, {"-addr", "invalid-address", "-public-url", "http://localhost:8090"},
		{"-addr", "127.0.0.1:0", "-public-url", "invalid-url"},
		{"-addr", "127.0.0.1:0", "-public-url", "http://localhost:8090", "-fps", "0", "-width", "321"},
	} {
		if err := run(context.Background(), args, io.Discard); err == nil {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
}

func TestCommandServesAndShutsDown(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	address := "127.0.0.1:" + strconv.Itoa(port)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-addr", address, "-public-url", "http://" + address, "-work-dir", t.TempDir()}, io.Discard)
	}()
	client := http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, e := client.Get("http://" + address + "/healthz")
		if e == nil {
			res.Body.Close()
			if res.StatusCode != 200 {
				t.Fatal(res.Status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("CLI not ready: %v", e)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CLI shutdown timed out")
	}
	l, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatalf("CLI retained listening socket: %v", err)
	}
	l.Close()
}
