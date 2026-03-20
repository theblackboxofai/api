package chat

import (
	"context"
	"database/sql"
)

const listCandidateServersQuery = `
WITH recent_servers AS (
  SELECT DISTINCT ON (ss.server_url)
    ss.server_url,
    ss.scanned_at
  FROM server_scans ss
  JOIN server_models sm ON sm.server_scan_id = ss.id
  WHERE ss.scanned_at >= NOW() - INTERVAL '24 hours'
    AND COALESCE(NULLIF(sm.model, ''), sm.name) = $1
  ORDER BY ss.server_url, ss.scanned_at DESC, ss.id DESC
)
SELECT server_url
FROM recent_servers
ORDER BY server_url;
`

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) ListCandidateServers(ctx context.Context, rawModelID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, listCandidateServersQuery, rawModelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []string
	for rows.Next() {
		var serverURL string
		if err := rows.Scan(&serverURL); err != nil {
			return nil, err
		}
		servers = append(servers, serverURL)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return servers, nil
}
