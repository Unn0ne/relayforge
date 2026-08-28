package delivery

import (
	"errors"
	"fmt"
	"time"
)

type Decision string

const (
	DecisionSucceed Decision = "succeed"
	DecisionRetry   Decision = "retry"
	DecisionDiscard Decision = "discard"
)

type RetryPolicy struct {
	base   time.Duration
	max    time.Duration
	jitter float64
}

func NewRetryPolicy(base, max time.Duration, jitter float64) (RetryPolicy, error) {
	if base <= 0 {
		return RetryPolicy{}, errors.New("base delay must be positive")
	}
	if max < base {
		return RetryPolicy{}, errors.New("max delay must not be less than base delay")
	}
	if jitter < 0 || jitter > 1 {
		return RetryPolicy{}, errors.New("jitter must be between 0 and 1")
	}
	return RetryPolicy{base: base, max: max, jitter: jitter}, nil
}

func (p RetryPolicy) NextDelay(attempt int, sample float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	delay := p.base
	for current := 1; current < attempt; current++ {
		if delay >= p.max/2 {
			delay = p.max
			break
		}
		delay *= 2
	}

	if delay > p.max {
		delay = p.max
	}
	if sample < 0 {
		sample = 0
	}
	if sample > 1 {
		sample = 1
	}

	factor := 1 + (sample*2-1)*p.jitter
	result := time.Duration(float64(delay) * factor)
	if result > p.max {
		return p.max
	}
	return result
}

func Evaluate(statusCode int, requestErr error) Decision {
	if requestErr != nil {
		return DecisionRetry
	}
	if statusCode >= 200 && statusCode < 300 {
		return DecisionSucceed
	}
	if statusCode == 408 || statusCode == 425 || statusCode == 429 || statusCode >= 500 {
		return DecisionRetry
	}
	return DecisionDiscard
}

func ValidateMaxAttempts(attempts int) error {
	if attempts < 1 || attempts > 100 {
		return fmt.Errorf("max attempts must be between 1 and 100")
	}
	return nil
}
