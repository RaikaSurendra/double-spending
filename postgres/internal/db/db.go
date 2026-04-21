package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const DSN = "postgres://postgres:postgres@localhost:5432/doublespending?sslmode=disable"

func Connect(ctx context.Context) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, DSN)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

// Seed resets account 1 to the given balance and zeroes the version counter.
func Seed(ctx context.Context, pool *pgxpool.Pool, balance int) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, balance, version) VALUES (1, $1, 0)
		ON CONFLICT (id) DO UPDATE SET balance = $1, version = 0
	`, balance)
	return err
}
