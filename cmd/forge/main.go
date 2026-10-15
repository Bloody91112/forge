package main

import (
	"fmt"
	"forge/internal/config"
	"log"
	"os"
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

	fmt.Println("Forge starting...")

	_ = conf

	return nil
}
