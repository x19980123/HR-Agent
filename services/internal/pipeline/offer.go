package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hr-agent/services/internal/audit"
	"github.com/hr-agent/services/internal/db"
)

var validOfferStatuses = map[string]bool{
	"none": true, "pending": true, "sent": true,
	"accepted": true, "declined": true, "hired": true,
}

type OfferUpdateInput struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

func (s *Service) UpdateOfferStatus(ctx context.Context, appID string, in OfferUpdateInput) error {
	status := strings.ToLower(strings.TrimSpace(in.Status))
	if !validOfferStatuses[status] || status == "none" {
		return fmt.Errorf("status must be pending|sent|accepted|declined|hired")
	}
	var cur string
	if err := s.DB.QueryRowContext(ctx, `SELECT offer_status FROM applications WHERE id=?`, appID).Scan(&cur); err != nil {
		return err
	}
	cur = strings.ToLower(strings.TrimSpace(cur))
	if cur == "" {
		cur = "none"
	}
	if !offerTransitionOK(cur, status) {
		return fmt.Errorf("cannot transition offer %s → %s", cur, status)
	}
	note := strings.TrimSpace(in.Note)
	var err error
	if status == "hired" {
		_, err = s.DB.ExecContext(ctx,
			`UPDATE applications SET offer_status=?, offer_note=?, offer_updated_at=NOW(), hired_at=? WHERE id=?`,
			status, note, time.Now(), appID,
		)
	} else {
		_, err = s.DB.ExecContext(ctx,
			`UPDATE applications SET offer_status=?, offer_note=?, offer_updated_at=NOW() WHERE id=?`,
			status, note, appID,
		)
	}
	if err != nil {
		return err
	}
	after := ""
	if status == "hired" {
		after = db.StatusConfirmed
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "offer_" + status,
		AfterStatus:   after,
		Detail:        map[string]any{"from": cur, "to": status, "note": in.Note},
	})
	return nil
}

func offerTransitionOK(from, to string) bool {
	if from == to {
		return true
	}
	allowed := map[string][]string{
		"none":     {"pending"},
		"pending":  {"sent", "declined"},
		"sent":     {"accepted", "declined", "pending"},
		"accepted": {"hired", "declined"},
		"declined": {"pending", "sent"},
		"hired":    {},
	}
	for _, x := range allowed[from] {
		if x == to {
			return true
		}
	}
	// allow HR to set pending from none-like empty, or jump to pending after all rounds
	if to == "pending" && (from == "none" || from == "") {
		return true
	}
	return false
}

func (s *Service) setOfferPendingIfNeeded(ctx context.Context, appID string) error {
	var st string
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(offer_status,'none') FROM applications WHERE id=?`, appID).Scan(&st)
	if st != "" && st != "none" {
		return nil
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE applications SET offer_status='pending', offer_updated_at=NOW() WHERE id=? AND (offer_status='none' OR offer_status='' OR offer_status IS NULL)`,
		appID,
	)
	if err != nil {
		return err
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "offer_pending",
		AfterStatus:   db.StatusConfirmed,
		Detail:        map[string]any{"reason": "all_rounds_passed"},
	})
	return nil
}
