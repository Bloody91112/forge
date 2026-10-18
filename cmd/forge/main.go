package main

import (
	"context"
	"errors"
	"fmt"
	"forge/internal/config"
	"forge/internal/database"
	"forge/internal/handler"
	"forge/internal/postgres"
	"forge/internal/task"
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

	health := handler.HealthHandler{}
	readiness := handler.NewReadinessHandler(dbPool)

	taskRepo := postgres.NewTaskRepository(dbPool)
	taskService := task.NewService(taskRepo)
	taskHandler := handler.NewTaskHandler(taskService)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Check)
	mux.HandleFunc("GET /readyz", readiness.Check)
	mux.HandleFunc("POST /api/v1/tasks", taskHandler.CreateTask)
	mux.HandleFunc("POST /api/v1/tasks/{id}", taskHandler.GetTask)

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
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("http server stopped: %w", err)
		}
	}

	readiness.SetNotReady()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)

		if closeErr := server.Close(); closeErr != nil {
			log.Printf("force close failed: %v", closeErr)
		}
	}

	return runErr
}
