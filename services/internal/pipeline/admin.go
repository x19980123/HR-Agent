package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hr-agent/services/internal/replytoken"
)

type JDInput struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Department   string         `json:"department"`
	Salary       string         `json:"salary"`
	Location     string         `json:"location"`
	Description  string         `json:"description"` // 岗位说明 / 职责 / 要求
	Requirements any            `json:"requirements"`
	Weights      map[string]any `json:"weights"`
	// RequirementsText optional structured paste stored into requirements.raw_text
	RequirementsText string `json:"requirements_text"`
}

func (s *Service) ListJDs(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, title, department, COALESCE(salary,''), COALESCE(location,''),
		        COALESCE(description,''), requirements, weight_json, created_at
		 FROM job_descriptions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, title, dept, salary, loc, desc string
		var req, weight []byte
		var created time.Time
		if err := rows.Scan(&id, &title, &dept, &salary, &loc, &desc, &req, &weight, &created); err != nil {
			return nil, err
		}
		item := map[string]any{
			"id": id, "title": title, "department": dept,
			"salary": salary, "location": loc, "description": desc,
			"requirements": jsonRaw(string(req)), "weights": jsonRaw(string(weight)),
			"created_at": created,
		}
		if rounds, rerr := s.ListJDRounds(ctx, id); rerr == nil {
			item["rounds"] = rounds
			item["round_count"] = len(rounds)
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) GetJD(ctx context.Context, id string) (map[string]any, error) {
	var title, dept, salary, loc, desc string
	var req, weight []byte
	var created time.Time
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, title, department, COALESCE(salary,''), COALESCE(location,''),
		        COALESCE(description,''), requirements, weight_json, created_at
		 FROM job_descriptions WHERE id=?`, id,
	).Scan(&id, &title, &dept, &salary, &loc, &desc, &req, &weight, &created)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"id": id, "title": title, "department": dept,
		"salary": salary, "location": loc, "description": desc,
		"requirements": jsonRaw(string(req)), "weights": jsonRaw(string(weight)),
		"created_at": created,
	}
	rounds, rerr := s.ListJDRounds(ctx, id)
	if rerr != nil {
		return nil, rerr
	}
	out["rounds"] = rounds
	out["round_count"] = len(rounds)
	return out, nil
}

func (s *Service) UpsertJD(ctx context.Context, in JDInput) (string, error) {
	if strings.TrimSpace(in.Title) == "" {
		return "", fmt.Errorf("title required")
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = "jd-" + uuid.NewString()[:8]
	}
	req := in.Requirements
	if req == nil || req == "" {
		req = map[string]any{}
	}
	reqMap, ok := req.(map[string]any)
	if !ok {
		reqMap = map[string]any{}
	}
	if in.RequirementsText != "" {
		reqMap["raw_text"] = in.RequirementsText
	}
	if in.Description != "" {
		reqMap["description"] = in.Description
	}
	req = reqMap
	weights := in.Weights
	if weights == nil {
		weights = map[string]any{
			"education": 15, "major": 10, "years": 20, "skills": 35, "projects": 15, "papers": 5,
		}
	}
	reqJSON, _ := json.Marshal(req)
	wJSON, _ := json.Marshal(weights)
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO job_descriptions (id, title, department, salary, location, description, requirements, weight_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE title=VALUES(title), department=VALUES(department),
		   salary=VALUES(salary), location=VALUES(location), description=VALUES(description),
		   requirements=VALUES(requirements), weight_json=VALUES(weight_json)`,
		id, in.Title, in.Department, in.Salary, in.Location, in.Description, reqJSON, wJSON,
	)
	return id, err
}

func (s *Service) DeleteJD(ctx context.Context, id string) error {
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM applications WHERE jd_id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("该岗位下还有 %d 条申请，无法删除", n)
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM job_descriptions WHERE id=?`, id)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return fmt.Errorf("jd not found")
	}
	return nil
}

func (s *Service) ListApplications(ctx context.Context, status, errorKind string, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, jd_id, candidate_email, candidate_name, status, reschedule_count, resume_path,
	       COALESCE(error_kind,'none'), human_reason_code, system_error_code, created_at, updated_at
	       FROM applications WHERE 1=1`
	var args []any
	if status != "" {
		q += ` AND status=?`
		args = append(args, status)
	}
	if errorKind != "" {
		q += ` AND error_kind=?`
		args = append(args, errorKind)
	}
	q += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, jd, email, name, st, resumePath string
		var ek string
		var humanCode, systemCode sql.NullString
		var rc int
		var created, updated time.Time
		if err := rows.Scan(&id, &jd, &email, &name, &st, &rc, &resumePath, &ek, &humanCode, &systemCode, &created, &updated); err != nil {
			return nil, err
		}
		item := map[string]any{
			"id": id, "jd_id": jd, "candidate_email": email, "candidate_name": name,
			"status": st, "reschedule_count": rc, "created_at": created, "updated_at": updated,
			"has_resume": resumePath != "", "error_kind": ek,
		}
		if humanCode.Valid {
			item["human_reason_code"] = humanCode.String
		}
		if systemCode.Valid {
			item["system_error_code"] = systemCode.String
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) GetApplicationDetail(ctx context.Context, appID string) (map[string]any, error) {
	app, err := s.GetApplication(ctx, appID)
	if err != nil {
		return nil, err
	}
	slots, _ := s.loadPublicSlots(ctx, appID)
	app["slots"] = slots

	if jdID, _ := app["jd_id"].(string); jdID != "" {
		if jd, err := s.GetJD(ctx, jdID); err == nil {
			app["jd"] = jd
		}
	}

	rows, err := s.DB.QueryContext(ctx,
		`SELECT action, actor, before_status, after_status, detail_json, created_at
		 FROM audit_events WHERE application_id=? ORDER BY id DESC LIMIT 50`, appID)
	if err == nil {
		defer rows.Close()
		var audits []map[string]any
		for rows.Next() {
			var action, actor string
			var before, after, detail sql.NullString
			var created time.Time
			_ = rows.Scan(&action, &actor, &before, &after, &detail, &created)
			item := map[string]any{"action": action, "actor": actor, "created_at": created}
			if before.Valid {
				item["before_status"] = before.String
			}
			if after.Valid {
				item["after_status"] = after.String
			}
			if detail.Valid {
				item["detail"] = jsonRaw(detail.String)
			}
			audits = append(audits, item)
		}
		app["audit"] = audits
	}
	if links, lerr := s.BuildInterviewerPackLinks(ctx, appID); lerr == nil && len(links) > 0 {
		pack := make([]map[string]any, 0, len(links))
		for _, lk := range links {
			pack = append(pack, map[string]any{
				"open_id": lk.OpenID, "name": lk.Name, "email": lk.Email,
				"url": lk.URL, "expires_at": lk.ExpiresAt,
			})
		}
		app["interviewer_pack"] = pack
	}
	return app, nil
}

func (s *Service) GetResumePath(ctx context.Context, appID string) (string, error) {
	var path string
	err := s.DB.QueryRowContext(ctx, `SELECT resume_path FROM applications WHERE id=?`, appID).Scan(&path)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("no resume file")
	}
	resolved := s.resolveResumePath(path)
	if resolved == "" {
		return "", fmt.Errorf("no resume file")
	}
	return resolved, nil
}

func (s *Service) Stats(ctx context.Context) (map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM applications GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byStatus := map[string]int{}
	total := 0
	for rows.Next() {
		var st string
		var n int
		_ = rows.Scan(&st, &n)
		byStatus[st] = n
		total += n
	}
	var last7 int
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM applications WHERE created_at >= (NOW() - INTERVAL 7 DAY)`,
	).Scan(&last7)
	return map[string]any{
		"total":           total,
		"by_status":       byStatus,
		"created_last_7d": last7,
		"awaiting_reply":  byStatus["awaiting_reply"],
		"confirmed":       byStatus["confirmed"],
		"declined":        byStatus["declined"],
		"needs_human":     byStatus["needs_human"],
		"rejected":        byStatus["rejected"],
	}, nil
}

func (s *Service) PingDB(ctx context.Context) error {
	return s.DB.PingContext(ctx)
}

func (s *Service) IssueReplyToken(appID string) (string, error) {
	return replytoken.Issue(s.Cfg.ReplyTokenSecret, appID, s.Cfg.ReplyTimeout())
}
