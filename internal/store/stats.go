package store

import (
	"context"
	"fmt"
)

type OperationalStats struct {
	Pending      int64
	Processing   int64
	Retrying     int64
	Succeeded    int64
	Dead         int64
	OpenCircuits int64
}

func (s *Store) GetOperationalStats(ctx context.Context) (OperationalStats, error) {
	var result OperationalStats
	err := s.pool.QueryRow(ctx, `
        SELECT
            count(*) FILTER (WHERE status = 'pending'),
            count(*) FILTER (WHERE status = 'processing'),
            count(*) FILTER (WHERE status = 'retrying'),
            count(*) FILTER (WHERE status = 'succeeded'),
            count(*) FILTER (WHERE status = 'dead'),
            (SELECT count(*) FROM endpoints WHERE circuit_open_until > now())
        FROM deliveries`).Scan(
		&result.Pending,
		&result.Processing,
		&result.Retrying,
		&result.Succeeded,
		&result.Dead,
		&result.OpenCircuits,
	)
	if err != nil {
		return OperationalStats{}, fmt.Errorf("query operational stats: %w", err)
	}
	return result, nil
}
