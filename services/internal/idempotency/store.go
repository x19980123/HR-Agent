package idempotency

import (
	"context"
	"database/sql"
	"strings"
)

// Acquire records a side-effect key; false if key already exists.
func Acquire(ctx context.Context, db *sql.DB, key, action, applicationID string) (bool, error) {
	if key == "" {
		return true, nil
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO idempotency_keys (idem_key, action, application_id) VALUES (?, ?, ?)`,
		key, action, applicationID,
	)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "1062") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
