package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/endpoint"
	"github.com/jackc/pgx/v5"
)

const endpointColumns = `
    id::text,
    name,
    url,
    secret_ciphertext,
    timeout_ms,
    max_attempts,
    consecutive_failures,
    circuit_open_until,
    disabled_at,
    created_at,
    updated_at`

func (s *Store) CreateEndpoint(ctx context.Context, value delivery.Endpoint) (delivery.Endpoint, error) {
	row := s.pool.QueryRow(ctx, `
        INSERT INTO endpoints (
            id, name, url, secret_ciphertext, timeout_ms, max_attempts
        )
        VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING `+endpointColumns,
		value.ID,
		value.Name,
		value.URL,
		value.SecretCiphertext,
		value.Timeout.Milliseconds(),
		value.MaxAttempts,
	)

	created, err := scanEndpoint(row)
	if err != nil {
		return delivery.Endpoint{}, fmt.Errorf("insert endpoint: %w", err)
	}
	return created, nil
}

func (s *Store) ListEndpoints(ctx context.Context, limit int) ([]delivery.Endpoint, error) {
	rows, err := s.pool.Query(ctx, `
        SELECT `+endpointColumns+`
        FROM endpoints
        ORDER BY created_at DESC, id DESC
        LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("query endpoints: %w", err)
	}
	defer rows.Close()

	result := make([]delivery.Endpoint, 0, limit)
	for rows.Next() {
		value, scanErr := scanEndpoint(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan endpoint: %w", scanErr)
		}
		result = append(result, value)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoints: %w", err)
	}
	return result, nil
}

func (s *Store) GetEndpoint(ctx context.Context, id string) (delivery.Endpoint, error) {
	row := s.pool.QueryRow(ctx, `
        SELECT `+endpointColumns+`
        FROM endpoints
        WHERE id = $1`, id)

	result, err := scanEndpoint(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return delivery.Endpoint{}, endpoint.ErrNotFound
	}
	if err != nil {
		return delivery.Endpoint{}, fmt.Errorf("query endpoint: %w", err)
	}
	return result, nil
}

func (s *Store) DisableEndpoint(ctx context.Context, id string) (delivery.Endpoint, error) {
	row := s.pool.QueryRow(ctx, `
        UPDATE endpoints
        SET disabled_at = COALESCE(disabled_at, now()),
            updated_at = CASE WHEN disabled_at IS NULL THEN now() ELSE updated_at END
        WHERE id = $1
        RETURNING `+endpointColumns, id)

	result, err := scanEndpoint(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return delivery.Endpoint{}, endpoint.ErrNotFound
	}
	if err != nil {
		return delivery.Endpoint{}, fmt.Errorf("disable endpoint: %w", err)
	}
	return result, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanEndpoint(row rowScanner) (delivery.Endpoint, error) {
	var result delivery.Endpoint
	var timeoutMilliseconds int
	err := row.Scan(
		&result.ID,
		&result.Name,
		&result.URL,
		&result.SecretCiphertext,
		&timeoutMilliseconds,
		&result.MaxAttempts,
		&result.ConsecutiveFailures,
		&result.CircuitOpenUntil,
		&result.DisabledAt,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if err != nil {
		return delivery.Endpoint{}, err
	}
	result.Timeout = time.Duration(timeoutMilliseconds) * time.Millisecond
	return result, nil
}
