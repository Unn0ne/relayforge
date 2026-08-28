package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/jackc/pgx/v5"
)

const deliveryColumns = `
    d.id::text,
    d.event_id::text,
    d.endpoint_id::text,
    d.status,
    d.attempt_count,
    d.max_attempts,
    d.next_attempt_at,
    d.locked_by,
    d.lease_token::text,
    d.locked_at,
    d.locked_until,
    d.last_status_code,
    d.last_error,
    d.last_completed_at,
    d.created_at,
    d.updated_at`

func (s *Store) GetDelivery(ctx context.Context, id string) (delivery.Details, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return delivery.Details{}, fmt.Errorf("begin delivery read: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var result delivery.Details
	var payload []byte
	row := tx.QueryRow(ctx, `
        SELECT `+deliveryColumns+`,
            e.id::text,
            e.endpoint_id::text,
            e.event_type,
            e.payload,
            e.idempotency_key,
            e.created_at
        FROM deliveries d
        JOIN events e ON e.id = d.event_id
        WHERE d.id = $1`, id)
	if err = scanDelivery(row, &result.Delivery,
		&result.Event.ID,
		&result.Event.EndpointID,
		&result.Event.Type,
		&payload,
		&result.Event.IdempotencyKey,
		&result.Event.CreatedAt,
	); errors.Is(err, pgx.ErrNoRows) {
		return delivery.Details{}, delivery.ErrNotFound
	} else if err != nil {
		return delivery.Details{}, fmt.Errorf("query delivery: %w", err)
	}
	result.Event.Payload = json.RawMessage(payload)

	rows, err := tx.Query(ctx, `
        SELECT
            id::text,
            delivery_id::text,
            attempt_number,
            status_code,
            response_body,
            error_message,
            duration_ms,
            started_at,
            completed_at
        FROM delivery_attempts
        WHERE delivery_id = $1
        ORDER BY attempt_number`, id)
	if err != nil {
		return delivery.Details{}, fmt.Errorf("query delivery attempts: %w", err)
	}
	defer rows.Close()

	result.Attempts = make([]delivery.Attempt, 0, result.Delivery.AttemptCount)
	for rows.Next() {
		var attempt delivery.Attempt
		var durationMilliseconds int64
		if err = rows.Scan(
			&attempt.ID,
			&attempt.DeliveryID,
			&attempt.Number,
			&attempt.StatusCode,
			&attempt.ResponseBody,
			&attempt.ErrorMessage,
			&durationMilliseconds,
			&attempt.StartedAt,
			&attempt.CompletedAt,
		); err != nil {
			return delivery.Details{}, fmt.Errorf("scan delivery attempt: %w", err)
		}
		attempt.Duration = time.Duration(durationMilliseconds) * time.Millisecond
		result.Attempts = append(result.Attempts, attempt)
	}
	if err = rows.Err(); err != nil {
		return delivery.Details{}, fmt.Errorf("iterate delivery attempts: %w", err)
	}
	rows.Close()

	if err = tx.Commit(ctx); err != nil {
		return delivery.Details{}, fmt.Errorf("commit delivery read: %w", err)
	}
	return result, nil
}

func (s *Store) ReplayDelivery(ctx context.Context, id string) (delivery.Delivery, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return delivery.Delivery{}, fmt.Errorf("begin delivery replay: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var status delivery.Status
	var currentLimit int
	var replayBudget int
	var disabledAt *time.Time
	err = tx.QueryRow(ctx, `
        SELECT d.status, d.max_attempts, e.max_attempts, e.disabled_at
        FROM deliveries d
        JOIN endpoints e ON e.id = d.endpoint_id
        WHERE d.id = $1
        FOR UPDATE OF d, e`, id).Scan(&status, &currentLimit, &replayBudget, &disabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return delivery.Delivery{}, delivery.ErrNotFound
	}
	if err != nil {
		return delivery.Delivery{}, fmt.Errorf("lock delivery for replay: %w", err)
	}
	if disabledAt != nil {
		return delivery.Delivery{}, delivery.ErrEndpointDisabled
	}
	if status != delivery.StatusDead || currentLimit > math.MaxInt32-replayBudget {
		return delivery.Delivery{}, delivery.ErrNotReplayable
	}

	var result delivery.Delivery
	row := tx.QueryRow(ctx, `
        UPDATE deliveries d
        SET status = 'pending',
            max_attempts = $2,
            next_attempt_at = now(),
            updated_at = now()
        WHERE d.id = $1
        RETURNING `+deliveryColumns, id, currentLimit+replayBudget)
	if err = scanDelivery(row, &result); err != nil {
		return delivery.Delivery{}, fmt.Errorf("replay delivery: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return delivery.Delivery{}, fmt.Errorf("commit delivery replay: %w", err)
	}
	return result, nil
}

func scanDelivery(row rowScanner, result *delivery.Delivery, trailing ...any) error {
	var lockedBy *string
	var leaseToken *string
	destinations := []any{
		&result.ID,
		&result.EventID,
		&result.EndpointID,
		&result.Status,
		&result.AttemptCount,
		&result.MaxAttempts,
		&result.NextAttemptAt,
		&lockedBy,
		&leaseToken,
		&result.LockedAt,
		&result.LockedUntil,
		&result.LastStatusCode,
		&result.LastError,
		&result.LastCompletedAt,
		&result.CreatedAt,
		&result.UpdatedAt,
	}
	destinations = append(destinations, trailing...)
	if err := row.Scan(destinations...); err != nil {
		return err
	}
	if lockedBy != nil {
		result.LockedBy = *lockedBy
	}
	if leaseToken != nil {
		result.LeaseToken = *leaseToken
	}
	return nil
}
