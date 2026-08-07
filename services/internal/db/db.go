package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("mysql ping: %w", err)
	}
	return db, nil
}

// Status constants for applications.
const (
	StatusUploaded       = "uploaded"
	StatusParsing        = "parsing"
	StatusScreening      = "screening"
	StatusScreened       = "screened"
	StatusRejected       = "rejected"
	StatusQuestionsReady = "questions_ready"
	StatusScheduled      = "scheduled"
	StatusAwaitingReply  = "awaiting_reply"
	StatusConfirmed      = "confirmed"
	StatusDeclined       = "declined"
	StatusNeedsHuman     = "needs_human"
	StatusFailed         = "failed"
)
