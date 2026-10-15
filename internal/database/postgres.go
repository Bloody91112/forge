package database

import (
	"context"
	"forge/internal/config"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, conf config.Config) (*pgxpool.Pool, error) {
	dsn := (&url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(conf.DBUser, conf.DBPassword),
		Host:   net.JoinHostPort(conf.DBHost, strconv.Itoa(conf.DBPort)),
		Path:   "/" + conf.DBName,
	}).String()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}

	return pool, nil
}
