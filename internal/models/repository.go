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
