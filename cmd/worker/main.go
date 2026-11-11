package main

import (
	"context"
	"forge/internal/workeragent"
	"forge/internal/workeragent/config"
	"forge/internal/workeragent/forgeapi"
	"forge/internal/workeragent/task"
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
		log.Println(err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load()

	conf, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	forgeClient, err := forgeapi.NewClient(httpClient, conf.ForgeURL)
	if err != nil {
		return err
	}

	taskHandlers := map[string]task.ForgeTaskHandler{
		"sum-numbers": &task.NumbersSummer{},
	}

	taskService := task.NewService(taskHandlers)

	if err := taskService.ValidateCapabilities(conf.Capabilities); err != nil {
		return err
	}

	worker := workeragent.NewAgent(forgeClient, taskService, conf.BootstrapToken, conf.Capabilities)

	return worker.Run(ctx)
}
