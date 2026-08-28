package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestOperationalStatsIntegration(t *testing.T) {
	ctx := context.Background()
	repository, pool := openIntegrationStore(t)
	deliveryIDs := make([]string, 5)
	for index := range deliveryIDs {
		deliveryIDs[index] = seedDelivery(t, ctx, pool, 3)
	}

	_, err := pool.Exec(ctx, `
        UPDATE deliveries
        SET status = 'processing',
            locked_by = 'worker',
            lease_token = $2,
            locked_at = now(),
            locked_until = now() + interval '1 minute'
        WHERE id = $1`, deliveryIDs[1], uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	for index, status := range []string{"retrying", "succeeded", "dead"} {
		if _, err = pool.Exec(ctx, `UPDATE deliveries SET status = $2 WHERE id = $1`, deliveryIDs[index+2], status); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `
        UPDATE endpoints
        SET circuit_open_until = now() + interval '1 minute'
        WHERE id = (SELECT endpoint_id FROM deliveries WHERE id = $1)`, deliveryIDs[0]); err != nil {
		t.Fatal(err)
	}

	stats, err := repository.GetOperationalStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Pending != 1 || stats.Processing != 1 || stats.Retrying != 1 || stats.Succeeded != 1 || stats.Dead != 1 || stats.OpenCircuits != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}
