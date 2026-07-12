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

	"github.com/JoeanAmier/XHS-Downloader/internal/api"
)

func main() {
	cfg, err := api.ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	application, err := api.New(cfg, log.Default())
	if err != nil {
		log.Fatal(err)
	}
	defer application.Close()

	server := application.HTTPServer()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownDone := make(chan struct{})
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown: %v", err)
		}
		close(shutdownDone)
	}()

	log.Printf("XHS core API listening on http://%s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-shutdownDone
}
