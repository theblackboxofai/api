package models

import (
	"context"
	"database/sql"
)

const listCloudModelsQuery = `
SELECT
  COALESCE(NULLIF(sm.model, ''), sm.name) AS model_id,
  MAX(COALESCE(sm.modified_at, ss.scanned_at)) AS created_at
FROM server_models sm
JOIN server_scans ss ON ss.id = sm.server_scan_id
WHERE COALESCE(NULLIF(sm.model, ''), sm.name) LIKE '%' || $1 || '%'
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
    AND COALESCE(NULLIF(sm.model, ''), sm.name) LIKE '%' || $1 || '%'
  ORDER BY ss.server_url, COALESCE(NULLIF(sm.model, ''), sm.name), ss.scanned_at DESC, ss.id DESC
)
SELECT model_id, COUNT(*) AS server_count
FROM recent_model_servers
GROUP BY model_id
ORDER BY model_id;
`

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) ListCloudModels(ctx context.Context) ([]Record, error) {
	rows, err := r.db.QueryContext(ctx, listCloudModelsQuery, CloudTag)
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
	rows, err := r.db.QueryContext(ctx, listCloudModelStatsQuery, CloudTag)
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
