package config

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	ForgeURL       string
	BootstrapToken string
	Capabilities   []string
}

func Load() (Config, error) {
	forgeURL := getEnv("WORKER_FORGE_URL", "")
	if forgeURL == "" {
		return Config{}, errors.New("env WORKER_FORGE_URL not specified")
	}

	bootstrapToken := getEnv("WORKER_BOOTSTRAP_TOKEN", "")
	if bootstrapToken == "" {
		return Config{}, errors.New("env WORKER_BOOTSTRAP_TOKEN not specified")
	}

	capabilitiesString := getEnv("WORKER_CAPABILITIES", "")
	if capabilitiesString == "" {
		return Config{}, errors.New("env WORKER_CAPABILITIES not specified")
	}

	capabilities := make([]string, 0)

	seen := make(map[string]struct{})

	for _, value := range strings.Split(capabilitiesString, ",") {
		capability := strings.TrimSpace(value)

		if capability == "" {
			return Config{}, errors.New("env WORKER_CAPABILITIES contains empty value")
		}

		if _, exists := seen[capability]; exists {
			return Config{}, errors.New("env WORKER_CAPABILITIES contains duplicate values")
		}

		seen[capability] = struct{}{}
		capabilities = append(capabilities, capability)
	}

	return Config{
		ForgeURL:       forgeURL,
		BootstrapToken: bootstrapToken,
		Capabilities:   capabilities,
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
