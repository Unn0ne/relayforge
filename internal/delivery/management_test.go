package delivery

import (
	"context"
	"errors"
	"testing"
)

func TestManagementRejectsMalformedID(t *testing.T) {
	service := NewService(&deliveryRepositoryStub{})

	if _, err := service.Get(context.Background(), "invalid"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("get error = %v", err)
	}
	if _, err := service.Replay(context.Background(), "invalid"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("replay error = %v", err)
	}
}

type deliveryRepositoryStub struct{}

func (*deliveryRepositoryStub) GetDelivery(context.Context, string) (Details, error) {
	return Details{}, nil
}

func (*deliveryRepositoryStub) ReplayDelivery(context.Context, string) (Delivery, error) {
	return Delivery{}, nil
}
