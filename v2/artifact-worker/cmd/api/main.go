package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/devpablocristo/artifact-worker-v2/internal/adapters/out/processrunner"
	"github.com/devpablocristo/artifact-worker-v2/internal/adapters/out/toolchain"
	"github.com/devpablocristo/artifact-worker-v2/internal/extractor"
	"github.com/devpablocristo/artifact-worker-v2/internal/server"
)

func main() {
	token := strings.TrimSpace(os.Getenv("AXIS_V2_INTERNAL_AUTH_SECRET"))
	if token == "" {
		log.Fatal("AXIS_V2_INTERNAL_AUTH_SECRET is required")
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	profiles := toolchain.New(processrunner.Adapter{}, os.Getenv("ARTIFACT_WORKER_WHISPER_MODEL"), os.Getenv("ARTIFACT_WORKER_WHISPER_BIN"))
	service := extractor.NewService(profiles)
	concurrency := 2
	if configured, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ARTIFACT_WORKER_CONCURRENCY"))); err == nil && configured > 0 {
		concurrency = configured
	}
	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           server.NewHandlerWithConcurrency(service, token, concurrency).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	shutdownContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-shutdownContext.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}()
	log.Printf("artifact worker listening on :%s", port)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
