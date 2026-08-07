package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hr-agent/services/internal/audit"
)

type JoinRequestInput struct {
	OpenID string
	Name   string
	Email  string
	Avatar string
}

type ApproveJoinInput struct {
	IsHR          bool `json:"is_hr"`
	IsInterviewer bool `json:"is_interviewer"`
}

// SubmitJoinRequest creates or refreshes a pending join application after Feishu OAuth.
// Returns status: pending | already_member | already_pending | disabled | not_hr
func (s *Service) SubmitJoinRequest(ctx context.Context, in JoinRequestInput) (string, error) {
	openID := strings.TrimSpace(in.OpenID)
	if openID == "" {
		return "", fmt.Errorf("open_id required")
	}
	// Already an enabled HR → should have logged in; treat as member
	ok, err := s.IsHRAllowed(ctx, openID, in.Email)
	if err == nil && ok {
		return "already_member", nil
	}

	// Existing staff row: disabled must NOT create a new join request.
	existing, err := s.GetStaff(ctx, openID)
	if err == nil {
		enabled, _ := existing["enabled"].(bool)
		isHR, _ := existing["is_hr"].(bool)
		if !enabled {
			// Drop any stray pending applications for this open_id.
			_, _ = s.DB.ExecContext(ctx,
				`UPDATE staff_join_requests SET status='rejected', note='account_disabled', decided_by='system', decided_at=CURRENT_TIMESTAMP
				 WHERE open_id=? AND status='pending'`, openID)
			return "disabled", nil
		}
		if !isHR {
			return "not_hr", nil
		}
	} else if err != sql.ErrNoRows {
		return "", err
	}

	var pendingID string
	err = s.DB.QueryRowContext(ctx,
		`SELECT id FROM staff_join_requests WHERE open_id=? AND status='pending' ORDER BY created_at DESC LIMIT 1`,
		openID,
	).Scan(&pendingID)
	if err == nil && pendingID != "" {
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE staff_join_requests SET name=?, email=?, avatar=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
			strings.TrimSpace(in.Name), strings.TrimSpace(in.Email), strings.TrimSpace(in.Avatar), pendingID,
		)
		return "already_pending", nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}

	id := "jr-" + uuid.NewString()[:12]
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO staff_join_requests (id, open_id, name, email, avatar, status)
		 VALUES (?, ?, ?, ?, ?, 'pending')`,
		id, openID, strings.TrimSpace(in.Name), strings.TrimSpace(in.Email), strings.TrimSpace(in.Avatar),
	)
	if err != nil {
		return "", err
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: systemAppID,
		Action:        "staff_join_requested",
		Actor:         openID,
		Detail: map[string]any{
			"request_id": id, "open_id": openID, "name": in.Name, "email": in.Email,
		},
	})
	return "pending", nil
}

func (s *Service) ListJoinRequests(ctx context.Context, status string) ([]map[string]any, error) {
	status = strings.TrimSpace(status)
	var rows *sql.Rows
	var err error
	if status == "" || status == "all" {
		rows, err = s.DB.QueryContext(ctx,
			`SELECT id, open_id, COALESCE(name,''), COALESCE(email,''), COALESCE(avatar,''), status,
			        COALESCE(note,''), COALESCE(decided_by,''), decided_at, created_at, updated_at
			 FROM staff_join_requests ORDER BY FIELD(status,'pending','rejected','approved'), created_at DESC LIMIT 100`)
	} else {
		rows, err = s.DB.QueryContext(ctx,
			`SELECT id, open_id, COALESCE(name,''), COALESCE(email,''), COALESCE(avatar,''), status,
			        COALESCE(note,''), COALESCE(decided_by,''), decided_at, created_at, updated_at
			 FROM staff_join_requests WHERE status=? ORDER BY created_at DESC LIMIT 100`, status)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, openID, name, email, avatar, st, note, decidedBy string
		var decidedAt sql.NullTime
		var created, updated time.Time
		if err := rows.Scan(&id, &openID, &name, &email, &avatar, &st, &note, &decidedBy, &decidedAt, &created, &updated); err != nil {
			return nil, err
		}
		item := map[string]any{
			"id": id, "open_id": openID, "name": name, "email": email, "avatar": avatar,
			"status": st, "note": note, "decided_by": decidedBy,
			"created_at": created, "updated_at": updated,
		}
		if decidedAt.Valid {
			item["decided_at"] = decidedAt.Time
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) CountPendingJoinRequests(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM staff_join_requests WHERE status='pending'`,
	).Scan(&n)
	return n, err
}

func (s *Service) ApproveJoinRequest(ctx context.Context, requestID string, in ApproveJoinInput, actor string) (map[string]any, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, fmt.Errorf("request id required")
	}
	// Join applicants are HR users of this system.
	if !in.IsHR {
		in.IsHR = true
	}
	var openID, name, email string
	var status string
	err := s.DB.QueryRowContext(ctx,
		`SELECT open_id, COALESCE(name,''), COALESCE(email,''), status FROM staff_join_requests WHERE id=?`,
		requestID,
	).Scan(&openID, &name, &email, &status)
	if err != nil {
		return nil, fmt.Errorf("申请不存在")
	}
	if status != "pending" {
		return nil, fmt.Errorf("申请已处理（%s）", status)
	}

	isHR, isInt := in.IsHR, in.IsInterviewer
	enabled := true
	staff, err := s.UpsertStaff(ctx, StaffInput{
		OpenID: openID, Name: name, Email: email,
		IsHR: &isHR, IsInterviewer: &isInt, Enabled: &enabled,
	}, actor)
	if err != nil {
		return nil, err
	}

	_, err = s.DB.ExecContext(ctx,
		`UPDATE staff_join_requests SET status='approved', decided_by=?, decided_at=CURRENT_TIMESTAMP, note='approved' WHERE id=?`,
		actorOrSystem(actor), requestID,
	)
	if err != nil {
		return nil, err
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: systemAppID,
		Action:        "staff_join_approved",
		Actor:         actorOrSystem(actor),
		Detail: map[string]any{
			"request_id": requestID, "open_id": openID, "name": name,
			"is_hr": in.IsHR, "is_interviewer": in.IsInterviewer,
		},
	})
	return staff, nil
}

func (s *Service) RejectJoinRequest(ctx context.Context, requestID, note, actor string) error {
	requestID = strings.TrimSpace(requestID)
	var status string
	err := s.DB.QueryRowContext(ctx,
		`SELECT status FROM staff_join_requests WHERE id=?`, requestID,
	).Scan(&status)
	if err != nil {
		return fmt.Errorf("申请不存在")
	}
	if status != "pending" {
		return fmt.Errorf("申请已处理（%s）", status)
	}
	if strings.TrimSpace(note) == "" {
		note = "rejected"
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE staff_join_requests SET status='rejected', decided_by=?, decided_at=CURRENT_TIMESTAMP, note=? WHERE id=?`,
		actorOrSystem(actor), strings.TrimSpace(note), requestID,
	)
	if err != nil {
		return err
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: systemAppID,
		Action:        "staff_join_rejected",
		Actor:         actorOrSystem(actor),
		Detail:        map[string]any{"request_id": requestID, "note": note},
	})
	return nil
}
