package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Unn0ne/relayforge/internal/endpoint"
	"github.com/Unn0ne/relayforge/internal/eventing"
	"github.com/jackc/pgx/v5"
)

func (s *Store) EnqueueEvent(ctx context.Context, params eventing.EnqueueParams) (eventing.Enqueued, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return eventing.Enqueued{}, fmt.Errorf("begin enqueue transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var endpointMaxAttempts int
	var disabledAt *time.Time
	err = tx.QueryRow(ctx, `
        SELECT max_attempts, disabled_at
        FROM endpoints
        WHERE id = $1
        FOR SHARE`, params.EndpointID).Scan(&endpointMaxAttempts, &disabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return eventing.Enqueued{}, endpoint.ErrNotFound
	}
	if err != nil {
		return eventing.Enqueued{}, fmt.Errorf("lock endpoint: %w", err)
	}
	if disabledAt != nil {
		return eventing.Enqueued{}, eventing.ErrEndpointDisabled
	}

	var result eventing.Enqueued
	var payload []byte
	var payloadMatches bool
	err = tx.QueryRow(ctx, `
        INSERT INTO events (
            id, endpoint_id, event_type, payload, idempotency_key
        )
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (endpoint_id, idempotency_key)
        DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key
        RETURNING
            id::text,
            endpoint_id::text,
            event_type,
            payload,
            idempotency_key,
            created_at,
            event_type = $3 AND payload = $4::jsonb`,
		params.EventID,
		params.EndpointID,
		params.Type,
		params.Payload,
		params.IdempotencyKey,
	).Scan(
		&result.Event.ID,
		&result.Event.EndpointID,
		&result.Event.Type,
		&payload,
		&result.Event.IdempotencyKey,
		&result.Event.CreatedAt,
		&payloadMatches,
	)
	if err != nil {
		return eventing.Enqueued{}, fmt.Errorf("upsert event: %w", err)
	}
	if !payloadMatches {
		return eventing.Enqueued{}, eventing.ErrIdempotencyConflict
	}
	result.Event.Payload = json.RawMessage(payload)
	result.Duplicate = result.Event.ID != params.EventID

	var lastStatusCode *int
	var lockedBy *string
	var leaseToken *string
	err = tx.QueryRow(ctx, `
        INSERT INTO deliveries (
            id, event_id, endpoint_id, max_attempts
        )
        VALUES ($1, $2, $3, $4)
        ON CONFLICT (event_id)
        DO UPDATE SET event_id = EXCLUDED.event_id
        RETURNING
            id::text,
            event_id::text,
            endpoint_id::text,
            status,
            attempt_count,
            max_attempts,
            next_attempt_at,
            locked_by,
            lease_token::text,
            locked_at,
            locked_until,
            last_status_code,
            last_error,
            last_completed_at,
            created_at,
            updated_at`,
		params.DeliveryID,
		result.Event.ID,
		params.EndpointID,
		endpointMaxAttempts,
	).Scan(
		&result.Delivery.ID,
		&result.Delivery.EventID,
		&result.Delivery.EndpointID,
		&result.Delivery.Status,
		&result.Delivery.AttemptCount,
		&result.Delivery.MaxAttempts,
		&result.Delivery.NextAttemptAt,
		&lockedBy,
		&leaseToken,
		&result.Delivery.LockedAt,
		&result.Delivery.LockedUntil,
		&lastStatusCode,
		&result.Delivery.LastError,
		&result.Delivery.LastCompletedAt,
		&result.Delivery.CreatedAt,
		&result.Delivery.UpdatedAt,
	)
	if err != nil {
		return eventing.Enqueued{}, fmt.Errorf("upsert delivery: %w", err)
	}
	if lastStatusCode != nil {
		result.Delivery.LastStatusCode = *lastStatusCode
	}
	if lockedBy != nil {
		result.Delivery.LockedBy = *lockedBy
	}
	if leaseToken != nil {
		result.Delivery.LeaseToken = *leaseToken
	}

	if err = tx.Commit(ctx); err != nil {
		return eventing.Enqueued{}, fmt.Errorf("commit event enqueue: %w", err)
	}
	return result, nil
}
