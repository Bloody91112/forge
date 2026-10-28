package main

import (
	"context"
	"errors"
	"fmt"
	"forge/internal/auth"
	"forge/internal/bootstrap"
	"forge/internal/config"
	"forge/internal/database"
	"forge/internal/handler"
	"forge/internal/middleware"
	"forge/internal/postgres"
	"forge/internal/settings"
	"forge/internal/task"
	"forge/internal/worker"
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

	args := os.Args[1:]

	if len(args) == 0 {
		return runServer()
	}

	switch args[0] {
	case "help":
		printUsage()
		return nil
	case "bootstrap":
		return runBootstrapCommand(args[1:])
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func runServer() error {
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

	settingsRepo := postgres.NewSettingRepository(dbPool)
	settingsService := settings.NewService(settingsRepo)
	bootstrapService := bootstrap.NewService(settingsService)

	workerRepo := postgres.NewWorkerRepository(dbPool)
	workerService := worker.NewService(workerRepo, bootstrapService)
	workerHandler := handler.NewWorkerHandler(workerService)
	workerAuthService := auth.NewWorkerService(workerRepo)

	taskRepo := postgres.NewTaskRepository(dbPool)
	taskService := task.NewService(taskRepo, workerRepo)
	taskHandler := handler.NewTaskHandler(taskService)

	clientRepository := postgres.NewClientRepository(dbPool)
	authService := auth.NewClientService(clientRepository)

	// public (no auth)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Check)
	mux.HandleFunc("GET /readyz", readiness.Check)
	mux.HandleFunc("POST /internal/v1/workers/register", workerHandler.Register)

	// internal
	workerMux := http.NewServeMux()
	workerMux.HandleFunc("POST /internal/v1/workers/heartbeat", workerHandler.Heartbeat)
	workerMux.HandleFunc("POST /internal/v1/tasks/claim", taskHandler.ClaimTask)

	mux.Handle("/internal/v1/", middleware.WorkerAuth(workerAuthService, workerMux))

	// public (auth)
	clientMux := http.NewServeMux()
	clientMux.HandleFunc("POST /api/v1/tasks", taskHandler.CreateTask)
	clientMux.HandleFunc("GET /api/v1/tasks/{id}", taskHandler.GetTask)
	mux.Handle("/api/v1/", middleware.Auth(authService, clientMux))

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

func printUsage() {
	fmt.Println(`Usage:
  forge                          Start the HTTP server
  forge help                     Show this help
  forge bootstrap init           Initialize the bootstrap token
  forge bootstrap rotate         Rotate the bootstrap token`)
}

func runBootstrapCommand(args []string) error {
	if len(args) != 1 {
		return errors.New("expected exactly 1 bootstrap command, type help")
	}

	command := bootstrap.Command(args[0])
	if command != bootstrap.CommandInit && command != bootstrap.CommandRotate {
		return fmt.Errorf("unknown bootstrap command: %s", command)
	}

	_ = godotenv.Load()

	conf, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	dbPool, err := database.Open(ctx, conf)
	if err != nil {
		return err
	}
	defer dbPool.Close()

	settingsRepo := postgres.NewSettingRepository(dbPool)
	settingsService := settings.NewService(settingsRepo)
	bootstrapService := bootstrap.NewService(settingsService)

	result, err := bootstrapService.Execute(ctx, command)
	if err != nil {
		return err
	}

	if result.AlreadyInitialized {
		log.Println("Bootstrap token already initialized")
		return nil
	}

	log.Println("Bootstrap token: " + result.Token)
	return nil
}
