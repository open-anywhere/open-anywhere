package casting

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

// Serve owns listener until shutdown. Refresh must return when its context is
// canceled. A separate goroutine refreshes targets immediately and periodically.
func Serve(ctx context.Context, listener net.Listener, handler http.Handler, interval time.Duration, refresh func(context.Context)) error {
	defer listener.Close()
	if interval <= 0 {
		return ErrInvalid
	}
	ctx, cancel := context.WithCancel(ctx)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	workers.Go(func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	})
	if refresh != nil {
		workers.Go(func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				refresh(ctx)
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		})
	}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
