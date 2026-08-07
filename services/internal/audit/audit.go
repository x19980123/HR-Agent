package audit

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/hr-agent/services/internal/pii"
)

type Event struct {
	ApplicationID   string
	Action          string
	Actor           string
	BeforeStatus    string
	AfterStatus     string
	Detail          map[string]any
	IdempotencyKey  string
	LangsmithRunID  string
}

type Logger struct {
	DB *sql.DB
}

func (l *Logger) Log(ctx context.Context, e Event) error {
	if e.Actor == "" {
		e.Actor = "system"
	}
	var detail any
	if e.Detail != nil {
		// redact string values
		safe := map[string]any{}
		for k, v := range e.Detail {
			if s, ok := v.(string); ok {
				safe[k] = pii.Redact(s)
			} else {
				safe[k] = v
			}
		}
		b, _ := json.Marshal(safe)
		detail = string(b)
	}
	_, err := l.DB.ExecContext(ctx,
		`INSERT INTO audit_events
		 (application_id, action, actor, before_status, after_status, detail_json, idempotency_key, langsmith_run_id)
		 VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, ''), NULLIF(?, ''))`,
		e.ApplicationID, e.Action, e.Actor, e.BeforeStatus, e.AfterStatus, detail, e.IdempotencyKey, e.LangsmithRunID,
	)
	return err
}
