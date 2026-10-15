package main

import (
	"context"
	"errors"
	"fmt"
	"forge/internal/config"
	"forge/internal/database"
	"forge/internal/handler"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load()

	conf, err := config.Load()
	if err != nil {
		return err
	}

	mainCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	dbPool, err := database.Open(mainCtx, conf)
	if err != nil {
		return err
	}
	defer dbPool.Close()

	healthinessHandler := handler.HealthinessHandler{}
	readinessHandler := handler.NewReadinessHandler(dbPool)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthinessHandler.Check)
	mux.HandleFunc("GET /readyz", readinessHandler.Check)

	server := &http.Server{
		Addr:              conf.HTTPAddr,
		Handler:           mux,
		ReadTimeout:       2 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serverErr := make(chan error, 1)

	go func() {
		serverErr <- server.ListenAndServe()
	}()

	var runErr error

	select {
	case <-mainCtx.Done():
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("http server stopped: %w", err)
		}
	}

	readinessHandler.SetNotReady()

	sdCtx, sdCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sdCancel()

	serverSdDone := make(chan struct{})

	go func() {
		defer close(serverSdDone)

		if err := server.Shutdown(sdCtx); err != nil {
			log.Printf("failed to shutdown server: %v", err)
			if closeErr := server.Close(); closeErr != nil {
				log.Printf("failed to force-close server: %v", closeErr)
			}
		} else {
			log.Printf("server stopped gracefully")
		}
	}()

	select {
	case <-serverSdDone:
	case <-sdCtx.Done():
		log.Printf("timed out waiting for server shutdown")
	}

	return runErr
}
