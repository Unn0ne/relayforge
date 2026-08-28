package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrLeaseLost = errors.New("delivery lease lost")

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}
