package delivery

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var (
	ErrInvalid          = errors.New("invalid delivery")
	ErrNotFound         = errors.New("delivery not found")
	ErrNotReplayable    = errors.New("delivery is not replayable")
	ErrEndpointDisabled = errors.New("delivery endpoint is disabled")
)

type Details struct {
	Delivery Delivery
	Event    Event
	Attempts []Attempt
}

type Repository interface {
	GetDelivery(context.Context, string) (Details, error)
	ReplayDelivery(context.Context, string) (Delivery, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Get(ctx context.Context, id string) (Details, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Details{}, fmt.Errorf("%w: malformed delivery id", ErrInvalid)
	}
	result, err := s.repository.GetDelivery(ctx, id)
	if err != nil {
		return Details{}, fmt.Errorf("get delivery: %w", err)
	}
	return result, nil
}

func (s *Service) Replay(ctx context.Context, id string) (Delivery, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Delivery{}, fmt.Errorf("%w: malformed delivery id", ErrInvalid)
	}
	result, err := s.repository.ReplayDelivery(ctx, id)
	if err != nil {
		return Delivery{}, fmt.Errorf("replay delivery: %w", err)
	}
	return result, nil
}
