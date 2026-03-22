package chat

import (
	"context"
	"database/sql"

	"blackbox-api/internal/models"
)

const serverScoreStep = 0.025

const listCandidateServersQuery = `
WITH recent_servers AS (
  SELECT DISTINCT ON (ss.server_url)
    ss.server_url,
    ss.scanned_at
  FROM server_scans ss
  JOIN server_models sm ON sm.server_scan_id = ss.id
  WHERE ss.scanned_at >= NOW() - INTERVAL '24 hours'
    AND COALESCE(NULLIF(sm.model, ''), sm.name) = $1
    AND sm.raw_json ->> 'remote_host' = $2
  ORDER BY ss.server_url, ss.scanned_at DESC, ss.id DESC
),
server_scores AS (
  SELECT
    l.server_url,
    1.0::double precision + COALESCE(SUM((CASE WHEN l.success THEN 1 ELSE -1 END) * $3::double precision), 0.0) AS score
  FROM logs l
  GROUP BY l.server_url
),
recent_request_load AS (
  SELECT
    l.server_url,
    COUNT(*) AS request_count
  FROM logs l
  WHERE l.created_at >= NOW() - INTERVAL '1 minute'
  GROUP BY l.server_url
)
SELECT rs.server_url
FROM recent_servers rs
LEFT JOIN server_scores sc ON sc.server_url = rs.server_url
LEFT JOIN recent_request_load rl ON rl.server_url = rs.server_url
ORDER BY (COALESCE(sc.score, 1.0::double precision) - COALESCE(rl.request_count, 0)::double precision * $3::double precision) DESC, rs.scanned_at DESC, rs.server_url;
`

const insertLogQuery = `
INSERT INTO logs (
  request_id,
  requested_model,
  raw_model_id,
  server_url,
  stream,
  success,
  response_status,
  request_json,
  response_headers,
  response_body,
  error_text
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11
);
`

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) ListCandidateServers(ctx context.Context, rawModelID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, listCandidateServersQuery, rawModelID, models.OllamaRemoteHost, serverScoreStep)
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

func (r *PostgresRepository) InsertLog(ctx context.Context, entry LogEntry) error {
	var responseStatus any
	if entry.ResponseStatus > 0 {
		responseStatus = entry.ResponseStatus
	}

	var responseHeaders any
	if len(entry.ResponseHeaders) > 0 {
		responseHeaders = []byte(entry.ResponseHeaders)
	}

	var responseBody any
	if entry.ResponseBody != "" {
		responseBody = entry.ResponseBody
	}

	var errorText any
	if entry.ErrorText != "" {
		errorText = entry.ErrorText
	}

	_, err := r.db.ExecContext(
		ctx,
		insertLogQuery,
		entry.RequestID,
		entry.RequestedModel,
		entry.RawModelID,
		entry.ServerURL,
		entry.Stream,
		entry.Success,
		responseStatus,
		[]byte(entry.RequestJSON),
		responseHeaders,
		responseBody,
		errorText,
	)
	return err
}
