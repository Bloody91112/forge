package main

import (
	"context"
	"forge/internal/config"
	"forge/internal/database"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
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

	return nil
}
