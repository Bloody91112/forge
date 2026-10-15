package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const DefaultDBHost string = "127.0.0.1"
const DefaultDBPort string = "5432"
const DefaultHTTPAddr string = ":8080"

type Config struct {
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string
	HTTPAddr   string
}

func Load() (Config, error) {
	dbHost := getEnv("DB_HOST", DefaultDBHost)
	dbPortStr := getEnv("DB_PORT", DefaultDBPort)
	httpAddr := getEnv("HTTP_ADDR", DefaultHTTPAddr)

	dbPort, err := strconv.Atoi(dbPortStr)
	if err != nil {
		return Config{}, fmt.Errorf("env DB_PORT not valid: %w", err)
	}

	if dbPort < 1 || dbPort > 65535 {
		return Config{}, fmt.Errorf("env DB_PORT not valid: %d", dbPort)
	}

	dbUser := getEnv("DB_USERNAME", "")
	if dbUser == "" {
		return Config{}, fmt.Errorf("env DB_USERNAME not specified")
	}

	dbPass := getEnv("DB_PASSWORD", "")
	if dbPass == "" {
		return Config{}, fmt.Errorf("env DB_PASSWORD not specified")
	}

	dbName := getEnv("DB_DATABASE", "")
	if dbName == "" {
		return Config{}, fmt.Errorf("env DB_DATABASE not specified")
	}

	return Config{
		DBHost:     dbHost,
		DBPort:     dbPort,
		DBUser:     dbUser,
		DBPassword: dbPass,
		DBName:     dbName,
		HTTPAddr:   httpAddr,
	}, nil

}

func getEnv(key string, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		return fallback
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}

	return value
}
