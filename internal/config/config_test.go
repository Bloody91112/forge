package config

import (
	"testing"
)

func TestLoad_LoadsWithDefaultEnvParams(t *testing.T) {
	dbName := "forge"
	dbUser := "user"
	dbPass := "pass"
	t.Setenv("DB_HOST", "")
	t.Setenv("DB_PORT", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DB_DATABASE", dbName)
	t.Setenv("DB_USERNAME", dbUser)
	t.Setenv("DB_PASSWORD", dbPass)

	conf, err := Load()

	if err != nil {
		t.Fatalf("load config err: %v", err)
	}

	if conf.DBHost != DefaultDBHost {
		t.Errorf("default db host was not used, expected: %s, got %s", DefaultDBHost, conf.DBHost)
	}

	if conf.HTTPAddr != DefaultHTTPAddr {
		t.Errorf("default http address was not used, expected: %s, got %s", DefaultHTTPAddr, conf.HTTPAddr)
	}

	if conf.DBPort != 5432 {
		t.Errorf("default db port was not used, expected: %d, got %d", 5432, conf.DBPort)
	}

	if dbName != conf.DBName {
		t.Errorf("unexpected db name, expected %s, got: %s", dbName, conf.DBName)
	}

	if dbUser != conf.DBUser {
		t.Errorf("unexpected db user, expected %s, got: %s", dbUser, conf.DBUser)
	}

	if dbPass != conf.DBPassword {
		t.Errorf("unexpected db pass, expected %s, got: %s", dbPass, conf.DBPassword)
	}
}

func TestLoad_ThrowsErrOnInvalidPort(t *testing.T) {
	setTestingEnv(t)
	t.Setenv("DB_PORT", "1234567")

	_, err := Load()

	if err == nil {
		t.Fatal("no expected error on invalid port")
	}
}

func TestLoad_ThrowsErrOnRequiredParams(t *testing.T) {
	tests := []struct {
		name string
		env  string
	}{
		{"required env db name", "DB_DATABASE"},
		{"required env db user", "DB_USERNAME"},
		{"required env db pass", "DB_PASSWORD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setTestingEnv(t)
			t.Setenv(tt.env, "")
			_, err := Load()
			if err == nil {
				t.Fatalf("no expected error on required param: %s", tt.env)
			}
		})
	}
}

func setTestingEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HTTP_ADDR", "localhost:8080")
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_DATABASE", "forge")
	t.Setenv("DB_USERNAME", "user")
	t.Setenv("DB_PASSWORD", "pass")
}
