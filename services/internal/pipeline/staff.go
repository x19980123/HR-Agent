package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/hr-agent/services/internal/audit"
	"github.com/hr-agent/services/internal/calendar"
)

const systemAppID = "system"

type StaffInput struct {
	OpenID        string `json:"open_id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	IsHR          *bool  `json:"is_hr"`
	IsInterviewer *bool  `json:"is_interviewer"`
	Enabled       *bool  `json:"enabled"`
}

func (s *Service) SeedAdminOpenID() string {
	return strings.TrimSpace(s.Cfg.FeishuInterviewerUserID)
}

func (s *Service) isSeedAdmin(openID string) bool {
	seed := s.SeedAdminOpenID()
	return seed != "" && strings.TrimSpace(openID) == seed
}

// IsSystemAdmin: only FEISHU_INTERVIEWER_USER_ID may manage staff (sole admin).
// Dev HR_API_TOKEN is treated as admin for local ops.
func (s *Service) IsSystemAdmin(ctx context.Context, openID string) bool {
	_ = ctx
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return false
	}
	if openID == "api_token" {
		return true
	}
	return s.isSeedAdmin(openID)
}

// BootstrapSeedAdmin upserts FEISHU_INTERVIEWER_* as the sole system admin.
func (s *Service) BootstrapSeedAdmin(ctx context.Context) error {
	openID := s.SeedAdminOpenID()
	if openID == "" {
		_ = s.RefreshInterviewerPool(ctx)
		return nil
	}
	name := strings.TrimSpace(s.Cfg.FeishuInterviewerName)
	if name == "" {
		name = "管理员"
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO staff_members (open_id, name, email, is_hr, is_interviewer, is_admin, enabled)
		 VALUES (?, ?, '', 1, 1, 1, 1)
		 ON DUPLICATE KEY UPDATE
		   name=IF(VALUES(name)<>'', VALUES(name), name),
		   is_hr=1, is_interviewer=1, is_admin=1, enabled=1`,
		openID, name,
	)
	if err != nil {
		return err
	}
	// Ensure only seed is admin
	_, _ = s.DB.ExecContext(ctx, `UPDATE staff_members SET is_admin=0 WHERE open_id<>?`, openID)
	return s.RefreshInterviewerPool(ctx)
}

func (s *Service) ListStaff(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT open_id, COALESCE(name,''), COALESCE(email,''), is_hr, is_interviewer, is_admin, enabled, created_at, updated_at
		 FROM staff_members ORDER BY is_admin DESC, is_hr DESC, is_interviewer DESC, updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		item, err := scanStaff(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) GetStaff(ctx context.Context, openID string) (map[string]any, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT open_id, COALESCE(name,''), COALESCE(email,''), is_hr, is_interviewer, is_admin, enabled, created_at, updated_at
		 FROM staff_members WHERE open_id=?`, openID)
	return scanStaff(row)
}

func scanStaff(row scannable) (map[string]any, error) {
	var openID, name, email string
	var isHR, isInterviewer, isAdmin, enabled bool
	var created, updated time.Time
	if err := row.Scan(&openID, &name, &email, &isHR, &isInterviewer, &isAdmin, &enabled, &created, &updated); err != nil {
		return nil, err
	}
	return map[string]any{
		"open_id": openID, "name": name, "email": email,
		"is_hr": isHR, "is_interviewer": isInterviewer, "is_admin": isAdmin, "enabled": enabled,
		"created_at": created, "updated_at": updated,
	}, nil
}

func (s *Service) countEnabledHR(ctx context.Context, excludeOpenID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM staff_members WHERE enabled=1 AND is_hr=1 AND open_id<>?`,
		excludeOpenID,
	).Scan(&n)
	return n, err
}

func (s *Service) UpsertStaff(ctx context.Context, in StaffInput, actor string) (map[string]any, error) {
	openID := strings.TrimSpace(in.OpenID)
	if openID == "" {
		return nil, fmt.Errorf("open_id required")
	}
	existing, err := s.GetStaff(ctx, openID)
	created := err == sql.ErrNoRows
	if err != nil && !created {
		return nil, err
	}

	isHR := true
	isInterviewer := true
	enabled := true
	if !created {
		isHR = existing["is_hr"].(bool)
		isInterviewer = existing["is_interviewer"].(bool)
		enabled = existing["enabled"].(bool)
	}
	if in.IsHR != nil {
		isHR = *in.IsHR
	}
	if in.IsInterviewer != nil {
		isInterviewer = *in.IsInterviewer
	}
	if in.Enabled != nil {
		enabled = *in.Enabled
	}

	// Sole system admin cannot be demoted / disabled via member UI
	isAdmin := s.isSeedAdmin(openID)
	if isAdmin {
		isHR = true
		enabled = true
		isInterviewer = true
	} else if !created {
		isAdmin = false
	}

	if !isHR && !isInterviewer {
		return nil, fmt.Errorf("至少选择 HR 或面试官之一")
	}

	if (!enabled || !isHR) && !created {
		wasHR := existing["is_hr"].(bool) && existing["enabled"].(bool)
		if wasHR {
			n, err := s.countEnabledHR(ctx, openID)
			if err != nil {
				return nil, err
			}
			if n == 0 {
				return nil, fmt.Errorf("不能停用或取消最后一个 HR")
			}
		}
	}

	name := strings.TrimSpace(in.Name)
	email := strings.TrimSpace(in.Email)
	if !created {
		if name == "" {
			name = existing["name"].(string)
		}
		if email == "" {
			email = existing["email"].(string)
		}
	}

	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO staff_members (open_id, name, email, is_hr, is_interviewer, is_admin, enabled)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), email=VALUES(email),
		   is_hr=VALUES(is_hr), is_interviewer=VALUES(is_interviewer),
		   is_admin=VALUES(is_admin), enabled=VALUES(enabled)`,
		openID, name, email, isHR, isInterviewer, isAdmin, enabled,
	)
	if err != nil {
		return nil, err
	}

	item, err := s.GetStaff(ctx, openID)
	if err != nil {
		return nil, err
	}

	action := "staff_updated"
	if created {
		action = "staff_created"
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: systemAppID,
		Action:        action,
		Actor:         actorOrSystem(actor),
		Detail: map[string]any{
			"open_id": openID, "name": name, "email": email,
			"is_hr": isHR, "is_interviewer": isInterviewer, "is_admin": isAdmin, "enabled": enabled,
		},
	})

	// Only HR get shared-calendar ACL via API; interviewers are managed in Feishu manually.
	_ = s.syncStaffCalendarACL(ctx, openID, enabled && isHR, actor)
	_ = s.RefreshInterviewerPool(ctx)
	return item, nil
}

func (s *Service) SetStaffEnabled(ctx context.Context, openID string, enabled bool, actor string) (map[string]any, error) {
	openID = strings.TrimSpace(openID)
	if s.isSeedAdmin(openID) && !enabled {
		return nil, fmt.Errorf("不能停用系统管理员")
	}
	existing, err := s.GetStaff(ctx, openID)
	if err != nil {
		return nil, err
	}
	if !enabled {
		wasHR := existing["is_hr"].(bool) && existing["enabled"].(bool)
		if wasHR {
			n, err := s.countEnabledHR(ctx, openID)
			if err != nil {
				return nil, err
			}
			if n == 0 {
				return nil, fmt.Errorf("不能停用最后一个 HR")
			}
		}
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE staff_members SET enabled=? WHERE open_id=?`, enabled, openID)
	if err != nil {
		return nil, err
	}
	if !enabled {
		// Prevent disabled users from reappearing as pending join applicants.
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE staff_join_requests SET status='rejected', note='account_disabled', decided_by=?, decided_at=CURRENT_TIMESTAMP
			 WHERE open_id=? AND status='pending'`,
			actorOrSystem(actor), openID,
		)
	}
	item, err := s.GetStaff(ctx, openID)
	if err != nil {
		return nil, err
	}
	action := "staff_enabled"
	if !enabled {
		action = "staff_disabled"
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: systemAppID,
		Action:        action,
		Actor:         actorOrSystem(actor),
		Detail: map[string]any{
			"open_id": openID, "name": item["name"], "enabled": enabled,
			"is_hr": item["is_hr"], "is_interviewer": item["is_interviewer"], "is_admin": item["is_admin"],
		},
	})
	// Interviewer calendar share is outside this system; only sync ACL for HR.
	wantACL := enabled && item["is_hr"].(bool)
	_ = s.syncStaffCalendarACL(ctx, openID, wantACL, actor)
	_ = s.RefreshInterviewerPool(ctx)
	return item, nil
}

func (s *Service) IsHRAllowed(ctx context.Context, openID, email string) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM staff_members`).Scan(&n)
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, sql.ErrNoRows
	}
	var ok int
	err = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM staff_members WHERE open_id=? AND enabled=1 AND is_hr=1`,
		openID,
	).Scan(&ok)
	if err != nil {
		return false, err
	}
	if ok > 0 {
		return true, nil
	}
	return false, nil
}

func (s *Service) TouchStaffProfile(ctx context.Context, openID, name, email string) {
	if strings.TrimSpace(openID) == "" {
		return
	}
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE staff_members SET
		   name=IF(?<>'', ?, name),
		   email=IF(?<>'', ?, email)
		 WHERE open_id=?`,
		name, name, email, email, openID,
	)
}

func (s *Service) ListInterviewerIDs(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT open_id FROM staff_members WHERE enabled=1 AND is_interviewer=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 && s.Cfg.FeishuInterviewerUserID != "" {
		ids = []string{s.Cfg.FeishuInterviewerUserID}
	}
	return ids, nil
}

func (s *Service) ListCalendarMemberIDs(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT open_id FROM staff_members WHERE enabled=1 AND (is_hr=1 OR is_interviewer=1)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Service) RefreshInterviewerPool(ctx context.Context) error {
	ids, err := s.ListInterviewerIDs(ctx)
	if err != nil {
		return err
	}
	if syncer, ok := s.Calendar.(calendar.StaffSyncer); ok {
		syncer.SetInterviewerUserIDs(ids)
	}
	return nil
}

func (s *Service) syncStaffCalendarACL(ctx context.Context, openID string, want bool, actor string) error {
	syncer, ok := s.Calendar.(calendar.StaffSyncer)
	if !ok {
		return nil
	}
	var err error
	if want {
		err = syncer.EnsureCalendarACL(ctx, openID)
	} else {
		err = syncer.RemoveCalendarACL(ctx, openID)
	}
	if err != nil {
		_ = s.Audit.Log(ctx, audit.Event{
			ApplicationID: systemAppID,
			Action:        "staff_calendar_acl_failed",
			Actor:         actorOrSystem(actor),
			Detail:        map[string]any{"open_id": openID, "want": want, "error": err.Error()},
		})
	}
	return err
}

func (s *Service) ListSystemAudit(ctx context.Context, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT action, actor, detail_json, created_at
		 FROM audit_events WHERE application_id=? ORDER BY id DESC LIMIT ?`,
		systemAppID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var action, actor string
		var detail []byte
		var created time.Time
		if err := rows.Scan(&action, &actor, &detail, &created); err != nil {
			return nil, err
		}
		item := map[string]any{
			"action": action, "actor": actor, "created_at": created,
			"detail": jsonRaw(string(detail)),
		}
		out = append(out, item)
	}
	return out, nil
}

func actorOrSystem(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return "system"
	}
	return actor
}
