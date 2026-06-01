package metrics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type EntityMetrics struct {
	EntityID          string    `json:"entity_id"`
	Timestamp         time.Time `json:"timestamp"`
	RequestRate       float64   `json:"request_rate"`
	ErrorRate         float64   `json:"error_rate"`
	P50LatencyMs      float64   `json:"p50_latency_ms"`
	P95LatencyMs      float64   `json:"p95_latency_ms"`
	P99LatencyMs      float64   `json:"p99_latency_ms"`
	CPUUtilization    float64   `json:"cpu_utilization"`
	MemoryUtilization float64   `json:"memory_utilization"`
}

type RelationshipMetrics struct {
	RelationshipID  string    `json:"relationship_id"`
	Timestamp       time.Time `json:"timestamp"`
	CallRate        float64   `json:"call_rate"`
	ErrorRate       float64   `json:"error_rate"`
	P99LatencyMs    float64   `json:"p99_latency_ms"`
	DataVolumeBytes int64     `json:"data_volume_bytes"`
}

type MetricsBatch struct {
	Entities      []EntityMetrics       `json:"entity_metrics"`
	Relationships []RelationshipMetrics `json:"relationship_metrics"`
}

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Ingest(ctx context.Context, batch MetricsBatch) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, em := range batch.Entities {
		if em.Timestamp.IsZero() {
			em.Timestamp = time.Now()
		}
		_, err := tx.ExecContext(ctx, `
            INSERT OR REPLACE INTO entity_metrics (
                entity_id, timestamp, request_rate, error_rate,
                p50_latency_ms, p95_latency_ms, p99_latency_ms,
                cpu_utilization, memory_utilization
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
        `,
			em.EntityID,
			em.Timestamp.UTC().Format(time.RFC3339Nano),
			em.RequestRate,
			em.ErrorRate,
			em.P50LatencyMs,
			em.P95LatencyMs,
			em.P99LatencyMs,
			em.CPUUtilization,
			em.MemoryUtilization,
		)
		if err != nil {
			return fmt.Errorf("insert entity metrics for %s: %w", em.EntityID, err)
		}
	}

	for _, rm := range batch.Relationships {
		if rm.Timestamp.IsZero() {
			rm.Timestamp = time.Now()
		}
		_, err := tx.ExecContext(ctx, `
            INSERT OR REPLACE INTO relationship_metrics (
                relationship_id, timestamp, call_rate, error_rate,
                p99_latency_ms, data_volume_bytes
            ) VALUES (?, ?, ?, ?, ?, ?)
        `,
			rm.RelationshipID,
			rm.Timestamp.UTC().Format(time.RFC3339Nano),
			rm.CallRate,
			rm.ErrorRate,
			rm.P99LatencyMs,
			rm.DataVolumeBytes,
		)
		if err != nil {
			return fmt.Errorf("insert relationship metrics for %s: %w", rm.RelationshipID, err)
		}
	}

	return tx.Commit()
}

func (s *Store) QueryEntityMetrics(ctx context.Context, entityID string, start, end time.Time) ([]EntityMetrics, error) {
	if start.IsZero() {
		start = time.Now().Add(-24 * time.Hour)
	}
	if end.IsZero() {
		end = time.Now()
	}

	rows, err := s.db.QueryContext(ctx, `
        SELECT entity_id, timestamp, request_rate, error_rate,
               p50_latency_ms, p95_latency_ms, p99_latency_ms,
               cpu_utilization, memory_utilization
          FROM entity_metrics
         WHERE entity_id = ? AND timestamp >= ? AND timestamp <= ?
         ORDER BY timestamp ASC
    `,
		entityID,
		start.UTC().Format(time.RFC3339Nano),
		end.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EntityMetrics
	for rows.Next() {
		var em EntityMetrics
		var ts string
		err := rows.Scan(
			&em.EntityID, &ts, &em.RequestRate, &em.ErrorRate,
			&em.P50LatencyMs, &em.P95LatencyMs, &em.P99LatencyMs,
			&em.CPUUtilization, &em.MemoryUtilization,
		)
		if err != nil {
			return nil, err
		}
		if em.Timestamp, err = time.Parse(time.RFC3339Nano, ts); err != nil {
			return nil, err
		}
		out = append(out, em)
	}
	return out, rows.Err()
}

func (s *Store) QueryRelationshipMetrics(ctx context.Context, relID string, start, end time.Time) ([]RelationshipMetrics, error) {
	if start.IsZero() {
		start = time.Now().Add(-24 * time.Hour)
	}
	if end.IsZero() {
		end = time.Now()
	}

	rows, err := s.db.QueryContext(ctx, `
        SELECT relationship_id, timestamp, call_rate, error_rate,
               p99_latency_ms, data_volume_bytes
          FROM relationship_metrics
         WHERE relationship_id = ? AND timestamp >= ? AND timestamp <= ?
         ORDER BY timestamp ASC
    `,
		relID,
		start.UTC().Format(time.RFC3339Nano),
		end.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RelationshipMetrics
	for rows.Next() {
		var rm RelationshipMetrics
		var ts string
		err := rows.Scan(
			&rm.RelationshipID, &ts, &rm.CallRate, &rm.ErrorRate,
			&rm.P99LatencyMs, &rm.DataVolumeBytes,
		)
		if err != nil {
			return nil, err
		}
		if rm.Timestamp, err = time.Parse(time.RFC3339Nano, ts); err != nil {
			return nil, err
		}
		out = append(out, rm)
	}
	return out, rows.Err()
}

func (s *Store) GetLatestEntityMetrics(ctx context.Context, entityID string) (*EntityMetrics, error) {
	row := s.db.QueryRowContext(ctx, `
        SELECT entity_id, timestamp, request_rate, error_rate,
               p50_latency_ms, p95_latency_ms, p99_latency_ms,
               cpu_utilization, memory_utilization
          FROM entity_metrics
         WHERE entity_id = ?
         ORDER BY timestamp DESC
         LIMIT 1
    `, entityID)

	var em EntityMetrics
	var ts string
	err := row.Scan(
		&em.EntityID, &ts, &em.RequestRate, &em.ErrorRate,
		&em.P50LatencyMs, &em.P95LatencyMs, &em.P99LatencyMs,
		&em.CPUUtilization, &em.MemoryUtilization,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if em.Timestamp, err = time.Parse(time.RFC3339Nano, ts); err != nil {
		return nil, err
	}
	return &em, nil
}
