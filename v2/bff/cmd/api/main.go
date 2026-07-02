package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/devpablocristo/bff-v2/wire"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	deps, err := wire.Initialize(ctx)
	if err != nil {
		log.Fatalf("initialize bff: %v", err)
	}
	defer deps.Close()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("bff listening on %s", deps.Server.Addr)
		errCh <- deps.Server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := deps.Server.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("shutdown bff: %v", err)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("run bff: %v", err)
		}
	}
}
