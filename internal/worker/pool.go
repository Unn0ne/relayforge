package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/store"
	"github.com/Unn0ne/relayforge/internal/webhook"
	"github.com/google/uuid"
)

type Queue interface {
	ClaimDelivery(context.Context, string, time.Duration) (*store.Job, error)
	FinishAttempt(context.Context, store.AttemptResult) (delivery.Status, error)
}

type Dispatcher interface {
	Deliver(context.Context, webhook.Request) webhook.Result
}

type Config struct {
	Concurrency      int
	PollInterval     time.Duration
	LeaseDuration    time.Duration
	FinishTimeout    time.Duration
	RetryPolicy      delivery.RetryPolicy
	CircuitThreshold int
	CircuitCooldown  time.Duration
}

type Pool struct {
	queue      Queue
	dispatcher Dispatcher
	logger     *slog.Logger
	config     Config
	instanceID string
	sample     func() float64
}

func New(queue Queue, dispatcher Dispatcher, logger *slog.Logger, config Config) (*Pool, error) {
	if config.Concurrency < 1 {
		return nil, errors.New("worker concurrency must be positive")
	}
	if config.PollInterval <= 0 || config.LeaseDuration <= 0 || config.FinishTimeout <= 0 {
		return nil, errors.New("worker durations must be positive")
	}
	if config.CircuitThreshold < 1 || config.CircuitCooldown <= 0 {
		return nil, errors.New("circuit breaker configuration is invalid")
	}
	return &Pool{
		queue:      queue,
		dispatcher: dispatcher,
		logger:     logger,
		config:     config,
		instanceID: uuid.NewString(),
		sample:     rand.Float64,
	}, nil
}

func (p *Pool) Run(ctx context.Context) {
	p.logger.Info("delivery workers started", "concurrency", p.config.Concurrency)
	var workers sync.WaitGroup
	workers.Add(p.config.Concurrency)
	for current := 0; current < p.config.Concurrency; current++ {
		workerID := fmt.Sprintf("%s/%d", p.instanceID, current+1)
		go func() {
			defer workers.Done()
			p.runWorker(ctx, workerID)
		}()
	}
	workers.Wait()
	p.logger.Info("delivery workers stopped")
}

func (p *Pool) runWorker(ctx context.Context, workerID string) {
	for {
		job, err := p.queue.ClaimDelivery(ctx, workerID, p.config.LeaseDuration)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			p.logger.Error("claim delivery", "worker_id", workerID, "error", err)
			if !wait(ctx, p.config.PollInterval) {
				return
			}
			continue
		}
		if job == nil {
			if !wait(ctx, p.config.PollInterval) {
				return
			}
			continue
		}

		if err = p.process(ctx, workerID, job); err != nil {
			if errors.Is(err, store.ErrLeaseLost) {
				p.logger.Warn("delivery lease lost", "delivery_id", job.Delivery.ID, "attempt", job.Delivery.AttemptCount)
			} else {
				p.logger.Error("finish delivery", "delivery_id", job.Delivery.ID, "attempt", job.Delivery.AttemptCount, "error", err)
			}
		}
	}
}

func (p *Pool) process(ctx context.Context, workerID string, job *store.Job) error {
	result := p.dispatcher.Deliver(ctx, webhook.Request{
		DeliveryID:       job.Delivery.ID,
		EventID:          job.Event.ID,
		EndpointID:       job.Endpoint.ID,
		EventType:        job.Event.Type,
		TargetURL:        job.Endpoint.URL,
		SecretCiphertext: job.Endpoint.SecretCiphertext,
		Payload:          job.Event.Payload,
		Timeout:          job.Endpoint.Timeout,
	})

	finish := store.AttemptResult{
		DeliveryID:    job.Delivery.ID,
		WorkerID:      workerID,
		LeaseToken:    job.Delivery.LeaseToken,
		AttemptNumber: job.Delivery.AttemptCount,
		Decision:      result.Decision,
		StatusCode:    result.StatusCode,
		ResponseBody:  result.ResponseBody,
		ErrorMessage:  result.ErrorMessage,
		Duration:      result.Duration,
		StartedAt:     result.StartedAt,
		CompletedAt:   result.CompletedAt,
	}
	if result.Decision == delivery.DecisionRetry {
		delay := p.config.RetryPolicy.NextDelay(job.Delivery.AttemptCount, p.sample())
		finish.NextAttemptAt = result.CompletedAt.Add(delay)
	}
	if ctx.Err() == nil {
		switch result.CircuitSignal {
		case webhook.CircuitFailure:
			finish.CircuitOutcome = store.CircuitFailure
			finish.CircuitThreshold = p.config.CircuitThreshold
			finish.CircuitCooldown = p.config.CircuitCooldown
		case webhook.CircuitSuccess:
			finish.CircuitOutcome = store.CircuitSuccess
		}
	}

	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.config.FinishTimeout)
	defer cancel()
	status, err := p.queue.FinishAttempt(finishCtx, finish)
	if err != nil {
		return err
	}
	p.logger.Info("delivery attempt completed",
		"delivery_id", job.Delivery.ID,
		"endpoint_id", job.Endpoint.ID,
		"attempt", job.Delivery.AttemptCount,
		"status", status,
		"status_code", result.StatusCode,
		"duration", result.Duration,
	)
	return nil
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
