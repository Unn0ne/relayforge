package eventing

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestPublish(t *testing.T) {
	repository := &eventRepositoryStub{}
	service := New(repository)
	endpointID := "0f4d9e5f-aac0-48d1-aa48-df706d70be39"

	_, err := service.Publish(context.Background(), PublishInput{
		EndpointID:     endpointID,
		Type:           " invoice.paid ",
		Payload:        json.RawMessage(`{"id":"invoice-id"}`),
		IdempotencyKey: " request-id ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.params.EndpointID != endpointID || repository.params.Type != "invoice.paid" {
		t.Fatalf("params = %+v", repository.params)
	}
	if repository.params.IdempotencyKey != "request-id" {
		t.Fatalf("idempotency key = %q", repository.params.IdempotencyKey)
	}
	if repository.params.EventID == "" || repository.params.DeliveryID == "" {
		t.Fatal("generated IDs are empty")
	}
}

func TestPublishValidation(t *testing.T) {
	valid := PublishInput{
		EndpointID:     "0f4d9e5f-aac0-48d1-aa48-df706d70be39",
		Type:           "invoice.paid",
		Payload:        json.RawMessage(`{"id":"invoice-id"}`),
		IdempotencyKey: "request-id",
	}

	tests := []struct {
		name   string
		mutate func(*PublishInput)
	}{
		{name: "endpoint id", mutate: func(input *PublishInput) { input.EndpointID = "invalid" }},
		{name: "event type", mutate: func(input *PublishInput) { input.Type = "invoice paid" }},
		{name: "empty payload", mutate: func(input *PublishInput) { input.Payload = nil }},
		{name: "invalid payload", mutate: func(input *PublishInput) { input.Payload = json.RawMessage(`{"id":`) }},
		{name: "large payload", mutate: func(input *PublishInput) {
			input.Payload = json.RawMessage(`"` + strings.Repeat("a", MaximumPayloadBytes) + `"`)
		}},
		{name: "empty idempotency key", mutate: func(input *PublishInput) { input.IdempotencyKey = "" }},
		{name: "control in idempotency key", mutate: func(input *PublishInput) { input.IdempotencyKey = "request\nkey" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := valid
			tt.mutate(&input)
			_, err := New(&eventRepositoryStub{}).Publish(context.Background(), input)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

type eventRepositoryStub struct {
	params EnqueueParams
	result Enqueued
	err    error
}

func (r *eventRepositoryStub) EnqueueEvent(_ context.Context, params EnqueueParams) (Enqueued, error) {
	r.params = params
	return r.result, r.err
}
