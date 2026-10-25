package portal

import (
	"context"
	"testing"
	"time"
)

func TestRegisterReturnsACancellationWhileWaitingForAHashSlot(t *testing.T) {
	_, auth, _, _ := newAuthDBEnv(t)
	for range maxHashConcurrency {
		auth.hashSem <- struct{}{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := auth.Register(ctx, Credentials{Username: "queued", Password: "password123"}, "agent", "10.0.0.1")
		done <- err
	}()
	// Let the account lookup finish before the wait for a hash slot is
	// interrupted; a wrapped lookup error would not match context.Canceled.
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("Register error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Register did not stop after cancellation")
	}
}
