package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Unn0ne/relayforge/internal/delivery"
	"github.com/Unn0ne/relayforge/internal/endpoint"
	"github.com/google/uuid"
)

func TestEndpointLifecycleIntegration(t *testing.T) {
	ctx := context.Background()
	store, _ := openIntegrationStore(t)
	id := uuid.NewString()

	created, err := store.CreateEndpoint(ctx, delivery.Endpoint{
		ID:               id,
		Name:             "Billing",
		URL:              "https://example.com/hooks",
		SecretCiphertext: []byte("ciphertext"),
		Timeout:          2500 * time.Millisecond,
		MaxAttempts:      6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != id || created.Timeout != 2500*time.Millisecond || created.MaxAttempts != 6 {
		t.Fatalf("created = %+v", created)
	}

	loaded, err := store.GetEndpoint(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "Billing" || string(loaded.SecretCiphertext) != "ciphertext" {
		t.Fatalf("loaded = %+v", loaded)
	}

	list, err := store.ListEndpoints(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != id {
		t.Fatalf("list = %+v", list)
	}

	disabled, err := store.DisableEndpoint(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.DisabledAt == nil {
		t.Fatal("endpoint was not disabled")
	}
	firstDisabledAt := *disabled.DisabledAt

	disabled, err = store.DisableEndpoint(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !disabled.DisabledAt.Equal(firstDisabledAt) {
		t.Fatalf("disabled timestamp changed from %s to %s", firstDisabledAt, disabled.DisabledAt)
	}

	_, err = store.GetEndpoint(ctx, uuid.NewString())
	if !errors.Is(err, endpoint.ErrNotFound) {
		t.Fatalf("error = %v", err)
	}
}
