package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hr-agent/services/internal/audit"
	"github.com/hr-agent/services/internal/db"
)

type JDRoundRequirementInput struct {
	RoleKind            string   `json:"role_kind"`
	Headcount           int      `json:"headcount"`
	PoolID              string   `json:"pool_id,omitempty"`
	MatchJDDepartment   bool     `json:"match_jd_department"`
	Specialties         []string `json:"specialties,omitempty"`
	FixedOpenIDs        []string `json:"fixed_open_ids,omitempty"`
}

type JDRoundInput struct {
	ID               string                    `json:"id,omitempty"`
	SortOrder        int                       `json:"sort_order"`
	RoundKey         string                    `json:"round_key"`
	Name             string                    `json:"name"`
	Theme            string                    `json:"theme"`
	DurationMinutes  int                       `json:"duration_minutes"`
	Advance          string                    `json:"advance"`
	Requirements     []JDRoundRequirementInput `json:"requirements"`
}

type InterviewPlanInput struct {
	Rounds []JDRoundInput `json:"rounds"`
}

func (s *Service) ListJDRounds(ctx context.Context, jdID string) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, sort_order, round_key, name, COALESCE(theme,''), duration_minutes, advance
		 FROM jd_interview_rounds WHERE jd_id=? ORDER BY sort_order`, jdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, key, name, theme, advance string
		var sortOrder, duration int
		if err := rows.Scan(&id, &sortOrder, &key, &name, &theme, &duration, &advance); err != nil {
			return nil, err
		}
		reqs, err := s.listRoundRequirements(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "sort_order": sortOrder, "round_key": key,
			"name": name, "theme": theme, "duration_minutes": duration,
			"advance": advance, "requirements": reqs,
		})
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

func (s *Service) listRoundRequirements(ctx context.Context, roundID string) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, role_kind, headcount, COALESCE(pool_id,''), match_jd_department, specialties, fixed_open_ids
		 FROM jd_round_interviewer_requirements WHERE jd_round_id=? ORDER BY role_kind, id`, roundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, role, pool string
		var headcount int
		var matchDept int
		var specs, fixed []byte
		if err := rows.Scan(&id, &role, &headcount, &pool, &matchDept, &specs, &fixed); err != nil {
			return nil, err
		}
		item := map[string]any{
			"id": id, "role_kind": role, "headcount": headcount,
			"match_jd_department": matchDept == 1,
		}
		if pool != "" {
			item["pool_id"] = pool
		}
		if len(specs) > 0 {
			item["specialties"] = jsonRaw(string(specs))
		} else {
			item["specialties"] = []any{}
		}
		if len(fixed) > 0 {
			item["fixed_open_ids"] = jsonRaw(string(fixed))
		} else {
			item["fixed_open_ids"] = []any{}
		}
		out = append(out, item)
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

// ReplaceJDRounds replaces the full interview plan for a JD in one transaction.
func (s *Service) ReplaceJDRounds(ctx context.Context, jdID string, in InterviewPlanInput) error {
	jdID = strings.TrimSpace(jdID)
	if jdID == "" {
		return fmt.Errorf("jd_id required")
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_descriptions WHERE id=?`, jdID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("jd not found")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM jd_round_interviewer_requirements WHERE jd_round_id IN (SELECT id FROM jd_interview_rounds WHERE jd_id=?)`,
		jdID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM jd_interview_rounds WHERE jd_id=?`, jdID); err != nil {
		return err
	}

	for i, r := range in.Rounds {
		sortOrder := i // UI order is canonical
		rid := strings.TrimSpace(r.ID)
		if rid == "" {
			rid = "jdr-" + uuid.NewString()[:12]
		}
		name := strings.TrimSpace(r.Name)
		if name == "" {
			name = fmt.Sprintf("第%d轮", i+1)
		}
		key := strings.TrimSpace(r.RoundKey)
		if key == "" {
			key = fmt.Sprintf("round_%d", i)
		}
		dur := r.DurationMinutes
		if dur <= 0 {
			dur = 60
		}
		advance := strings.TrimSpace(r.Advance)
		if advance == "" {
			advance = "hr_manual"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO jd_interview_rounds (id, jd_id, sort_order, round_key, name, theme, duration_minutes, advance)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			rid, jdID, sortOrder, key, name, r.Theme, dur, advance,
		); err != nil {
			return err
		}
		for _, req := range r.Requirements {
			role := strings.TrimSpace(req.RoleKind)
			if role == "" {
				role = "tech"
			}
			hc := req.Headcount
			if hc <= 0 {
				hc = 1
			}
			reqID := "jrr-" + uuid.NewString()[:12]
			specsJSON, _ := json.Marshal(req.Specialties)
			if req.Specialties == nil {
				specsJSON = []byte("[]")
			}
			fixed := cleanOpenIDs(req.FixedOpenIDs)
			fixedJSON, _ := json.Marshal(fixed)
			match := 0
			if req.MatchJDDepartment {
				match = 1
			}
			pool := strings.TrimSpace(req.PoolID)
			var poolAny any
			if pool != "" {
				poolAny = pool
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO jd_round_interviewer_requirements
				 (id, jd_round_id, role_kind, headcount, pool_id, match_jd_department, specialties, fixed_open_ids)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				reqID, rid, role, hc, poolAny, match, specsJSON, fixedJSON,
			); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func cleanOpenIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// ResolveAttendeesForRound fills each role_kind×headcount via fixed → pool → interviewer_profiles (by classification).
func (s *Service) ResolveAttendeesForRound(ctx context.Context, jdRoundID string) ([]string, map[string]any, error) {
	reqs, err := s.listRoundRequirements(ctx, jdRoundID)
	if err != nil {
		return nil, nil, err
	}
	if len(reqs) == 0 {
		return nil, nil, fmt.Errorf("interviewers_unassigned: no role requirements on round")
	}
	jdDept, _ := s.jdDepartment(ctx, jdRoundID)
	exclude := map[string]bool{}
	var all []string
	detail := map[string]any{"by_role": []any{}, "jd_department": jdDept}
	for _, req := range reqs {
		picked, roleDetail, err := s.resolveRoleAttendees(ctx, req, jdDept, exclude)
		if err != nil {
			return nil, nil, err
		}
		all = append(all, picked...)
		detail["by_role"] = append(detail["by_role"].([]any), roleDetail)
	}
	all = cleanOpenIDs(all)
	if len(all) == 0 {
		return nil, nil, fmt.Errorf("interviewers_unassigned: empty assignee list")
	}
	detail["assigned_open_ids"] = all
	detail["resolver"] = "classified_profiles"
	return all, detail, nil
}

func parseStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		var out []string
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case json.RawMessage:
		var out []string
		_ = json.Unmarshal(x, &out)
		return out
	default:
		// jsonRaw returns any from Unmarshal
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var out []string
		_ = json.Unmarshal(b, &out)
		return out
	}
}

func (s *Service) CountJDRounds(ctx context.Context, jdID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM jd_interview_rounds WHERE jd_id=?`, jdID).Scan(&n)
	return n, err
}

func (s *Service) GetJDRoundBySort(ctx context.Context, jdID string, sortOrder int) (roundID string, durationMin int, name string, err error) {
	err = s.DB.QueryRowContext(ctx,
		`SELECT id, duration_minutes, name FROM jd_interview_rounds WHERE jd_id=? AND sort_order=?`,
		jdID, sortOrder,
	).Scan(&roundID, &durationMin, &name)
	return
}

// EnsureApplicationRound creates application_interview_rounds row for index if missing.
func (s *Service) EnsureApplicationRound(ctx context.Context, appID, jdID string, roundIndex int) (appRoundID, jdRoundID string, duration time.Duration, err error) {
	var existing string
	err = s.DB.QueryRowContext(ctx,
		`SELECT id FROM application_interview_rounds WHERE application_id=? AND round_index=?`,
		appID, roundIndex,
	).Scan(&existing)
	if err == nil && existing != "" {
		var dur int
		var jdr sql.NullString
		_ = s.DB.QueryRowContext(ctx,
			`SELECT air.jd_round_id, COALESCE(jdr.duration_minutes, 60)
			 FROM application_interview_rounds air
			 LEFT JOIN jd_interview_rounds jdr ON jdr.id = air.jd_round_id
			 WHERE air.id=?`, existing,
		).Scan(&jdr, &dur)
		if dur <= 0 {
			dur = 60
		}
		jdRound := ""
		if jdr.Valid {
			jdRound = jdr.String
		}
		return existing, jdRound, time.Duration(dur) * time.Minute, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", "", 0, err
	}

	jdRoundID, durMin, _, err := s.GetJDRoundBySort(ctx, jdID, roundIndex)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", "", 0, fmt.Errorf("interview_plan_missing: JD 未配置第 %d 轮面试计划", roundIndex+1)
		}
		return "", "", 0, err
	}
	if durMin <= 0 {
		durMin = 60
	}
	appRoundID = "air-" + uuid.NewString()[:12]
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO application_interview_rounds (id, application_id, jd_round_id, round_index, status)
		 VALUES (?, ?, ?, ?, 'pending')`,
		appRoundID, appID, jdRoundID, roundIndex,
	)
	if err != nil {
		return "", "", 0, err
	}
	return appRoundID, jdRoundID, time.Duration(durMin) * time.Minute, nil
}

func (s *Service) ListApplicationRounds(ctx context.Context, appID string) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT air.id, air.round_index, air.status, air.jd_round_id, air.assigned_open_ids, air.assignment_detail,
		        air.outcome, air.provider_event_id, air.confirmed_slot_id, air.feedback_json,
		        COALESCE(jdr.name,''), COALESCE(jdr.theme,''), COALESCE(jdr.duration_minutes,60)
		 FROM application_interview_rounds air
		 LEFT JOIN jd_interview_rounds jdr ON jdr.id = air.jd_round_id
		 WHERE air.application_id=? ORDER BY air.round_index`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id string
		var idx int
		var status string
		var jdRoundID, outcome, eventID, slotID sql.NullString
		var assigned, detail, feedback []byte
		var name, theme string
		var duration int
		if err := rows.Scan(&id, &idx, &status, &jdRoundID, &assigned, &detail, &outcome, &eventID, &slotID, &feedback, &name, &theme, &duration); err != nil {
			return nil, err
		}
		item := map[string]any{
			"id": id, "round_index": idx, "status": status,
			"name": name, "theme": theme, "duration_minutes": duration,
		}
		if jdRoundID.Valid {
			item["jd_round_id"] = jdRoundID.String
		}
		if len(assigned) > 0 {
			item["assigned_open_ids"] = jsonRaw(string(assigned))
		}
		if len(detail) > 0 {
			item["assignment_detail"] = jsonRaw(string(detail))
		}
		if outcome.Valid {
			item["outcome"] = outcome.String
		}
		if eventID.Valid {
			item["provider_event_id"] = eventID.String
		}
		if slotID.Valid {
			item["confirmed_slot_id"] = slotID.String
		}
		if len(feedback) > 0 {
			item["feedback_json"] = jsonRaw(string(feedback))
		}
		out = append(out, item)
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

type AdvanceRoundInput struct {
	Outcome string `json:"outcome"` // pass | fail | hold
	Note    string `json:"note,omitempty"`
	// Optional HR scorecard (代录)
	Feedback *struct {
		Rating    int    `json:"rating,omitempty"`
		Summary   string `json:"summary,omitempty"`
		Recommend string `json:"recommend,omitempty"` // yes | no | weak
	} `json:"feedback,omitempty"`
}

// AdvanceRound records outcome for current/indexed round and starts next or rejects.
func (s *Service) AdvanceRound(ctx context.Context, appID string, roundIndex int, in AdvanceRoundInput) error {
	outcome := strings.ToLower(strings.TrimSpace(in.Outcome))
	if outcome != "pass" && outcome != "fail" && outcome != "hold" {
		return fmt.Errorf("outcome must be pass|fail|hold")
	}
	var status string
	var jdID string
	var curIdx int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT status, jd_id, current_round_index FROM applications WHERE id=?`, appID,
	).Scan(&status, &jdID, &curIdx); err != nil {
		return err
	}
	if roundIndex < 0 {
		roundIndex = curIdx
	}
	var appRoundID string
	var roundStatus string
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, status FROM application_interview_rounds WHERE application_id=? AND round_index=?`,
		appID, roundIndex,
	).Scan(&appRoundID, &roundStatus)
	if err != nil {
		return fmt.Errorf("application round not found: %w", err)
	}
	if roundStatus != "confirmed" {
		return fmt.Errorf("round must be confirmed before advance (status=%s)", roundStatus)
	}

	fbMap := map[string]any{"note": in.Note}
	if in.Feedback != nil {
		fbMap["rating"] = in.Feedback.Rating
		fbMap["summary"] = in.Feedback.Summary
		fbMap["recommend"] = in.Feedback.Recommend
		fbMap["by"] = "hr"
		fbMap["at"] = time.Now().Format(time.RFC3339)
	}
	fb, _ := json.Marshal(fbMap)
	_, err = s.DB.ExecContext(ctx,
		`UPDATE application_interview_rounds SET outcome=?, feedback_json=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		outcome, fb, appRoundID,
	)
	if err != nil {
		return err
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "round_" + outcome,
		AfterStatus:   status,
		Detail:        map[string]any{"round_index": roundIndex, "note": in.Note, "feedback": in.Feedback != nil},
	})

	switch outcome {
	case "fail":
		_, err = s.DB.ExecContext(ctx,
			`UPDATE applications SET status=?, error_message=?, error_kind=? WHERE id=?`,
			db.StatusRejected, "round_failed", "business", appID)
		return err
	case "hold":
		return s.markHumanWithCode(ctx, appID, "round_hold", "round_hold")
	case "pass":
		total, err := s.CountJDRounds(ctx, jdID)
		if err != nil {
			return err
		}
		next := roundIndex + 1
		if next >= total {
			_, err = s.DB.ExecContext(ctx,
				`UPDATE applications SET status=?, error_message=NULL WHERE id=?`,
				db.StatusConfirmed, appID)
			_ = s.Audit.Log(ctx, audit.Event{
				ApplicationID: appID,
				Action:        "all_rounds_passed",
				AfterStatus:   db.StatusConfirmed,
				Detail:        map[string]any{"rounds": total},
			})
			if err != nil {
				return err
			}
			return s.setOfferPendingIfNeeded(ctx, appID)
		}
		_, err = s.DB.ExecContext(ctx,
			`UPDATE applications SET current_round_index=?, status=?, reschedule_count=0, error_message=NULL WHERE id=?`,
			next, db.StatusScreened, appID,
		)
		if err != nil {
			return err
		}
		return s.scheduleAndNotify(ctx, appID, false, nil)
	}
	return nil
}
