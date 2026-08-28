package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const claimDeliveryQuery = `
WITH candidate AS (
    SELECT d.id
    FROM deliveries d
    JOIN endpoints e ON e.id = d.endpoint_id
    WHERE d.status IN ('pending', 'retrying')
      AND d.next_attempt_at <= now()
      AND d.attempt_count < d.max_attempts
      AND e.disabled_at IS NULL
      AND (e.circuit_open_until IS NULL OR e.circuit_open_until <= now())
    ORDER BY d.next_attempt_at, d.created_at
    FOR UPDATE OF d SKIP LOCKED
    LIMIT 1
), claimed AS (
    UPDATE deliveries d
    SET status = 'processing',
        attempt_count = d.attempt_count + 1,
        locked_by = $1,
        lease_token = $2,
        locked_at = now(),
        locked_until = now() + $3 * interval '1 millisecond',
        updated_at = now()
    FROM candidate c
    WHERE d.id = c.id
    RETURNING d.*
)
SELECT
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
    d.updated_at,
    ev.id::text,
    ev.endpoint_id::text,
    ev.event_type,
    ev.payload,
    ev.idempotency_key,
    ev.created_at,
    ep.id::text,
    ep.name,
    ep.url,
    ep.secret_ciphertext,
    ep.timeout_ms,
    ep.max_attempts,
    ep.consecutive_failures,
    ep.circuit_open_until,
    ep.disabled_at,
    ep.created_at,
    ep.updated_at
FROM claimed d
JOIN events ev ON ev.id = d.event_id
JOIN endpoints ep ON ep.id = d.endpoint_id`

const recoverExpiredQuery = `
WITH expired AS (
    SELECT d.id, d.locked_at
    FROM deliveries d
    WHERE d.status = 'processing'
      AND d.locked_until <= now()
    ORDER BY d.locked_until
    FOR UPDATE SKIP LOCKED
    LIMIT 32
)
UPDATE deliveries d
SET status = CASE WHEN d.attempt_count < d.max_attempts THEN 'retrying' ELSE 'dead' END,
    next_attempt_at = CASE WHEN d.attempt_count < d.max_attempts THEN now() ELSE d.next_attempt_at END,
    locked_by = NULL,
    lease_token = NULL,
    locked_at = NULL,
    locked_until = NULL,
    last_error = 'delivery lease expired',
    last_completed_at = now(),
    updated_at = now()
FROM expired e
WHERE d.id = e.id
RETURNING d.id::text, d.attempt_count, e.locked_at, d.last_completed_at`

type Job struct {
	Delivery delivery.Delivery
	Event    delivery.Event
	Endpoint delivery.Endpoint
}

type AttemptResult struct {
	DeliveryID    string
	WorkerID      string
	LeaseToken    string
	AttemptNumber int
	Decision      delivery.Decision
	StatusCode    *int
	ResponseBody  string
	ErrorMessage  string
	Duration      time.Duration
	StartedAt     time.Time
	CompletedAt   time.Time
	NextAttemptAt time.Time
}

type recoveredLease struct {
	deliveryID  string
	attempt     int
	startedAt   time.Time
	completedAt time.Time
}

func (s *Store) ClaimDelivery(ctx context.Context, workerID string, lease time.Duration) (*Job, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return nil, errors.New("worker id is required")
	}
	if lease < time.Millisecond {
		return nil, errors.New("lease duration must be at least one millisecond")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err = recoverExpiredLeases(ctx, tx); err != nil {
		return nil, err
	}

	job, err := scanJob(tx.QueryRow(ctx, claimDeliveryQuery, workerID, uuid.NewString(), lease.Milliseconds()))
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit lease recovery: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim delivery: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit delivery claim: %w", err)
	}
	return job, nil
}

func (s *Store) FinishAttempt(ctx context.Context, result AttemptResult) (delivery.Status, error) {
	if err := validateAttemptResult(result); err != nil {
		return "", err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin attempt transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var status delivery.Status
	err = tx.QueryRow(ctx, `
        UPDATE deliveries
        SET status = CASE
                WHEN $5 = 'succeed' THEN 'succeeded'
                WHEN $5 = 'retry' AND attempt_count < max_attempts THEN 'retrying'
                ELSE 'dead'
            END,
            next_attempt_at = CASE
                WHEN $5 = 'retry' AND attempt_count < max_attempts THEN $9
                ELSE next_attempt_at
            END,
            locked_by = NULL,
            lease_token = NULL,
            locked_at = NULL,
            locked_until = NULL,
            last_status_code = $6,
            last_error = $7,
            last_completed_at = $8,
            updated_at = now()
        WHERE id = $1
          AND status = 'processing'
          AND locked_by = $2
          AND lease_token = $3
          AND attempt_count = $4
        RETURNING status`,
		result.DeliveryID,
		result.WorkerID,
		result.LeaseToken,
		result.AttemptNumber,
		result.Decision,
		result.StatusCode,
		result.ErrorMessage,
		result.CompletedAt,
		result.NextAttemptAt,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrLeaseLost
	}
	if err != nil {
		return "", fmt.Errorf("finish delivery: %w", err)
	}

	_, err = tx.Exec(ctx, `
        INSERT INTO delivery_attempts (
            id, delivery_id, attempt_number, status_code, response_body,
            error_message, duration_ms, started_at, completed_at
        )
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		uuid.NewString(),
		result.DeliveryID,
		result.AttemptNumber,
		result.StatusCode,
		result.ResponseBody,
		result.ErrorMessage,
		durationMilliseconds(result.Duration),
		result.StartedAt,
		result.CompletedAt,
	)
	if err != nil {
		return "", fmt.Errorf("record delivery attempt: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit delivery attempt: %w", err)
	}
	return status, nil
}

func recoverExpiredLeases(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, recoverExpiredQuery)
	if err != nil {
		return fmt.Errorf("recover expired leases: %w", err)
	}

	recovered := make([]recoveredLease, 0, 32)
	for rows.Next() {
		var item recoveredLease
		if err = rows.Scan(&item.deliveryID, &item.attempt, &item.startedAt, &item.completedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan expired lease: %w", err)
		}
		recovered = append(recovered, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate expired leases: %w", err)
	}
	rows.Close()

	for _, item := range recovered {
		_, err = tx.Exec(ctx, `
            INSERT INTO delivery_attempts (
                id, delivery_id, attempt_number, error_message,
                duration_ms, started_at, completed_at
            )
            VALUES ($1, $2, $3, $4, $5, $6, $7)
            ON CONFLICT (delivery_id, attempt_number) DO NOTHING`,
			uuid.NewString(),
			item.deliveryID,
			item.attempt,
			"delivery lease expired",
			durationMilliseconds(item.completedAt.Sub(item.startedAt)),
			item.startedAt,
			item.completedAt,
		)
		if err != nil {
			return fmt.Errorf("record expired lease: %w", err)
		}
	}

	return nil
}

func scanJob(row pgx.Row) (*Job, error) {
	var job Job
	var payload []byte
	var timeoutMilliseconds int

	err := row.Scan(
		&job.Delivery.ID,
		&job.Delivery.EventID,
		&job.Delivery.EndpointID,
		&job.Delivery.Status,
		&job.Delivery.AttemptCount,
		&job.Delivery.MaxAttempts,
		&job.Delivery.NextAttemptAt,
		&job.Delivery.LockedBy,
		&job.Delivery.LeaseToken,
		&job.Delivery.LockedAt,
		&job.Delivery.LockedUntil,
		&job.Delivery.LastStatusCode,
		&job.Delivery.LastError,
		&job.Delivery.LastCompletedAt,
		&job.Delivery.CreatedAt,
		&job.Delivery.UpdatedAt,
		&job.Event.ID,
		&job.Event.EndpointID,
		&job.Event.Type,
		&payload,
		&job.Event.IdempotencyKey,
		&job.Event.CreatedAt,
		&job.Endpoint.ID,
		&job.Endpoint.Name,
		&job.Endpoint.URL,
		&job.Endpoint.SecretCiphertext,
		&timeoutMilliseconds,
		&job.Endpoint.MaxAttempts,
		&job.Endpoint.ConsecutiveFailures,
		&job.Endpoint.CircuitOpenUntil,
		&job.Endpoint.DisabledAt,
		&job.Endpoint.CreatedAt,
		&job.Endpoint.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	job.Event.Payload = json.RawMessage(payload)
	job.Endpoint.Timeout = time.Duration(timeoutMilliseconds) * time.Millisecond
	return &job, nil
}

func validateAttemptResult(result AttemptResult) error {
	if strings.TrimSpace(result.DeliveryID) == "" {
		return errors.New("delivery id is required")
	}
	if strings.TrimSpace(result.WorkerID) == "" {
		return errors.New("worker id is required")
	}
	if strings.TrimSpace(result.LeaseToken) == "" {
		return errors.New("lease token is required")
	}
	if result.AttemptNumber < 1 || int64(result.AttemptNumber) > math.MaxInt32 {
		return errors.New("attempt number is outside the supported range")
	}
	if result.Decision != delivery.DecisionSucceed && result.Decision != delivery.DecisionRetry && result.Decision != delivery.DecisionDiscard {
		return errors.New("invalid delivery decision")
	}
	if result.StatusCode != nil && (*result.StatusCode < 100 || *result.StatusCode > 599) {
		return errors.New("status code must be between 100 and 599")
	}
	if result.StatusCode == nil && strings.TrimSpace(result.ErrorMessage) == "" {
		return errors.New("error message is required without status code")
	}
	if result.StatusCode == nil && result.Decision != delivery.DecisionRetry {
		return errors.New("transport error must be retried")
	}
	if result.StatusCode != nil && delivery.Evaluate(*result.StatusCode, nil) != result.Decision {
		return errors.New("decision does not match status code")
	}
	if result.Duration < 0 {
		return errors.New("duration must not be negative")
	}
	if result.StartedAt.IsZero() || result.CompletedAt.IsZero() || result.CompletedAt.Before(result.StartedAt) {
		return errors.New("invalid attempt timestamps")
	}
	if result.Decision == delivery.DecisionRetry && !result.NextAttemptAt.After(result.CompletedAt) {
		return errors.New("next attempt must be after completion")
	}
	return nil
}

func durationMilliseconds(value time.Duration) int32 {
	milliseconds := value.Milliseconds()
	if milliseconds > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(milliseconds)
}
