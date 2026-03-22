package models

import (
	"context"
	"database/sql"
	"time"
)

const listCloudModelsQuery = `
SELECT
  COALESCE(NULLIF(sm.model, ''), sm.name) AS model_id,
  MAX(COALESCE(sm.modified_at, ss.scanned_at)) AS created_at
FROM server_models sm
JOIN server_scans ss ON ss.id = sm.server_scan_id
WHERE sm.raw_json ->> 'remote_host' = $1
GROUP BY COALESCE(NULLIF(sm.model, ''), sm.name)
ORDER BY COALESCE(NULLIF(sm.model, ''), sm.name);
`

const listCloudModelStatsQuery = `
WITH recent_model_servers AS (
  SELECT DISTINCT ON (ss.server_url, COALESCE(NULLIF(sm.model, ''), sm.name))
    COALESCE(NULLIF(sm.model, ''), sm.name) AS model_id,
    ss.server_url
  FROM server_scans ss
  JOIN server_models sm ON sm.server_scan_id = ss.id
  WHERE ss.scanned_at >= NOW() - INTERVAL '24 hours'
    AND sm.raw_json ->> 'remote_host' = $1
    AND NOT EXISTS (
      SELECT 1
      FROM logs l
      WHERE l.server_url = ss.server_url
        AND l.success = FALSE
        AND l.created_at >= NOW() - INTERVAL '6 hours'
    )
  ORDER BY ss.server_url, COALESCE(NULLIF(sm.model, ''), sm.name), ss.scanned_at DESC, ss.id DESC
)
SELECT model_id, COUNT(*) AS server_count
FROM recent_model_servers
GROUP BY model_id
ORDER BY model_id;
`

const listRequestHistoryQuery = `
SELECT
  created_at,
  request_id,
  success,
  response_body
FROM logs
WHERE created_at >= $1
ORDER BY created_at DESC, id DESC;
`

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) ListCloudModels(ctx context.Context) ([]Record, error) {
	rows, err := r.db.QueryContext(ctx, listCloudModelsQuery, OllamaRemoteHost)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []Record
	for rows.Next() {
		var record Record
		if err := rows.Scan(&record.ID, &record.CreatedAt); err != nil {
			return nil, err
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

func (r *PostgresRepository) ListCloudModelStats(ctx context.Context) ([]StatRecord, error) {
	rows, err := r.db.QueryContext(ctx, listCloudModelStatsQuery, OllamaRemoteHost)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []StatRecord
	for rows.Next() {
		var record StatRecord
		if err := rows.Scan(&record.ID, &record.ServerCount); err != nil {
			return nil, err
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

func (r *PostgresRepository) ListRequestHistory(ctx context.Context, since time.Time) ([]RequestHistoryRecord, error) {
	rows, err := r.db.QueryContext(ctx, listRequestHistoryQuery, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []RequestHistoryRecord
	for rows.Next() {
		var record RequestHistoryRecord
		var responseBody sql.NullString
		if err := rows.Scan(&record.CreatedAt, &record.RequestID, &record.Success, &responseBody); err != nil {
			return nil, err
		}
		if responseBody.Valid {
			record.ResponseBody = responseBody.String
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}
