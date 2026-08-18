package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hr-agent/services/internal/audit"
	"github.com/hr-agent/services/internal/calendar"
	"github.com/hr-agent/services/internal/db"
	"github.com/hr-agent/services/internal/mail"
	"github.com/hr-agent/services/internal/replytoken"
)

// Valid interviewer role kinds used in JD requirements and profiles.
var InterviewerRoleKinds = []string{"hr", "tech", "hm", "cross", "custom"}

type InterviewerProfileInput struct {
	OpenID      string   `json:"open_id"`
	Name        string   `json:"name"`
	Email       string   `json:"email"`
	Department  string   `json:"department"`
	RoleKinds   []string `json:"role_kinds"`
	Specialties []string `json:"specialties"`
	Enabled     *bool    `json:"enabled"`
	Notes       string   `json:"notes"`
}

type InterviewerPoolInput struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	DefaultRoleKind string   `json:"default_role_kind"`
	Department      string   `json:"department"`
	Enabled         *bool    `json:"enabled"`
	Notes           string   `json:"notes"`
	MemberOpenIDs   []string `json:"member_open_ids"`
}

func normalizeRoleKinds(kinds []string) []string {
	seen := map[string]bool{}
	var out []string
	allowed := map[string]bool{}
	for _, k := range InterviewerRoleKinds {
		allowed[k] = true
	}
	for _, k := range kinds {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" || !allowed[k] || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	if len(out) == 0 {
		out = []string{"tech"}
	}
	return out
}

func (s *Service) ListInterviewerProfiles(ctx context.Context, roleKind, department string) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT open_id, name, email, department, role_kinds, specialties, enabled, notes, updated_at
		 FROM interviewer_profiles ORDER BY department, name, open_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roleKind = strings.ToLower(strings.TrimSpace(roleKind))
	department = strings.TrimSpace(department)
	var out []map[string]any
	for rows.Next() {
		item, err := scanInterviewerProfile(rows)
		if err != nil {
			return nil, err
		}
		if roleKind != "" {
			ok := false
			for _, k := range parseStringSlice(item["role_kinds"]) {
				if strings.EqualFold(k, roleKind) {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		if department != "" && !strings.EqualFold(fmt.Sprint(item["department"]), department) {
			continue
		}
		out = append(out, item)
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

type profileScanner interface {
	Scan(dest ...any) error
}

func scanInterviewerProfile(row profileScanner) (map[string]any, error) {
	var openID, name, email, dept, notes string
	var roleKinds, specs []byte
	var enabled int
	var updated time.Time
	if err := row.Scan(&openID, &name, &email, &dept, &roleKinds, &specs, &enabled, &notes, &updated); err != nil {
		return nil, err
	}
	item := map[string]any{
		"open_id": openID, "name": name, "email": email, "department": dept,
		"enabled": enabled == 1, "notes": notes, "updated_at": updated,
	}
	if len(roleKinds) > 0 {
		item["role_kinds"] = jsonRaw(string(roleKinds))
	} else {
		item["role_kinds"] = []any{"tech"}
	}
	if len(specs) > 0 {
		item["specialties"] = jsonRaw(string(specs))
	} else {
		item["specialties"] = []any{}
	}
	return item, nil
}

func (s *Service) UpsertInterviewerProfile(ctx context.Context, in InterviewerProfileInput) (map[string]any, error) {
	openID := strings.TrimSpace(in.OpenID)
	if openID == "" {
		return nil, fmt.Errorf("open_id required")
	}
	kinds := normalizeRoleKinds(in.RoleKinds)
	specs := cleanOpenIDs(in.Specialties) // reuse trim/dedupe
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	kindsJSON, _ := json.Marshal(kinds)
	specsJSON, _ := json.Marshal(specs)
	en := 0
	if enabled {
		en = 1
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO interviewer_profiles (open_id, name, email, department, role_kinds, specialties, enabled, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), email=VALUES(email), department=VALUES(department),
		   role_kinds=VALUES(role_kinds), specialties=VALUES(specialties), enabled=VALUES(enabled), notes=VALUES(notes)`,
		openID, strings.TrimSpace(in.Name), strings.TrimSpace(in.Email), strings.TrimSpace(in.Department),
		kindsJSON, specsJSON, en, strings.TrimSpace(in.Notes),
	)
	if err != nil {
		return nil, err
	}
	row := s.DB.QueryRowContext(ctx,
		`SELECT open_id, name, email, department, role_kinds, specialties, enabled, notes, updated_at
		 FROM interviewer_profiles WHERE open_id=?`, openID)
	return scanInterviewerProfile(row)
}

func (s *Service) SetInterviewerProfileEnabled(ctx context.Context, openID string, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE interviewer_profiles SET enabled=? WHERE open_id=?`, en, openID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("profile not found")
	}
	return nil
}

func (s *Service) ListInterviewerPools(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, name, default_role_kind, department, enabled, notes, updated_at FROM interviewer_pools ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, name, role, dept, notes string
		var enabled int
		var updated time.Time
		if err := rows.Scan(&id, &name, &role, &dept, &enabled, &notes, &updated); err != nil {
			return nil, err
		}
		members, _ := s.listPoolMembers(ctx, id)
		out = append(out, map[string]any{
			"id": id, "name": name, "default_role_kind": role, "department": dept,
			"enabled": enabled == 1, "notes": notes, "updated_at": updated,
			"member_open_ids": members, "member_count": len(members),
		})
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

func (s *Service) listPoolMembers(ctx context.Context, poolID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT open_id FROM interviewer_pool_members WHERE pool_id=?`, poolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		out = append(out, id)
	}
	return out, nil
}

func (s *Service) UpsertInterviewerPool(ctx context.Context, in InterviewerPoolInput) (map[string]any, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("name required")
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = "pool-" + uuid.NewString()[:10]
	}
	role := strings.ToLower(strings.TrimSpace(in.DefaultRoleKind))
	if role == "" {
		role = "tech"
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	en := 0
	if enabled {
		en = 1
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO interviewer_pools (id, name, default_role_kind, department, enabled, notes)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE name=VALUES(name), default_role_kind=VALUES(default_role_kind),
		   department=VALUES(department), enabled=VALUES(enabled), notes=VALUES(notes)`,
		id, name, role, strings.TrimSpace(in.Department), en, strings.TrimSpace(in.Notes),
	)
	if err != nil {
		return nil, err
	}
	if in.MemberOpenIDs != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM interviewer_pool_members WHERE pool_id=?`, id); err != nil {
			return nil, err
		}
		for _, oid := range cleanOpenIDs(in.MemberOpenIDs) {
			// ensure profile exists (minimal stub)
			_, _ = tx.ExecContext(ctx,
				`INSERT IGNORE INTO interviewer_profiles (open_id, name, role_kinds, specialties, enabled)
				 VALUES (?, ?, ?, '[]', 1)`,
				oid, oid, `["`+role+`"]`,
			)
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO interviewer_pool_members (pool_id, open_id, role_kind) VALUES (?, ?, '')`,
				id, oid,
			); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	pools, err := s.ListInterviewerPools(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range pools {
		if p["id"] == id {
			return p, nil
		}
	}
	return map[string]any{"id": id}, nil
}

func (s *Service) DeleteInterviewerPool(ctx context.Context, poolID string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM interviewer_pools WHERE id=?`, poolID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("pool not found")
	}
	return nil
}

func specialtiesOverlap(need, have []string) bool {
	if len(need) == 0 {
		return true
	}
	set := map[string]bool{}
	for _, h := range have {
		set[strings.ToLower(strings.TrimSpace(h))] = true
	}
	for _, n := range need {
		if set[strings.ToLower(strings.TrimSpace(n))] {
			return true
		}
	}
	return false
}

func (s *Service) candidatesForRole(ctx context.Context, role, poolID, jdDept string, needSpecs []string, matchDept bool, exclude map[string]bool) ([]string, string, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	var ordered []string
	source := "profiles"

	if poolID != "" {
		source = "pool:" + poolID
		rows, err := s.DB.QueryContext(ctx,
			`SELECT m.open_id, COALESCE(NULLIF(m.role_kind,''), p.default_role_kind),
			        ip.department, ip.specialties, ip.enabled, ip.role_kinds
			 FROM interviewer_pool_members m
			 JOIN interviewer_pools p ON p.id = m.pool_id AND p.enabled=1
			 JOIN interviewer_profiles ip ON ip.open_id = m.open_id AND ip.enabled=1
			 WHERE m.pool_id=?`, poolID)
		if err != nil {
			return nil, source, err
		}
		defer rows.Close()
		for rows.Next() {
			var oid, mRole, dept string
			var specs, kinds []byte
			var enabled int
			if err := rows.Scan(&oid, &mRole, &dept, &specs, &enabled, &kinds); err != nil {
				continue
			}
			if exclude[oid] {
				continue
			}
			if !strings.EqualFold(mRole, role) {
				// also accept if profile role_kinds includes role
				ok := false
				for _, k := range parseStringSlice(jsonRaw(string(kinds))) {
					if strings.EqualFold(k, role) {
						ok = true
						break
					}
				}
				if !ok {
					continue
				}
			}
			if matchDept && jdDept != "" && !strings.EqualFold(dept, jdDept) {
				continue
			}
			have := parseStringSlice(jsonRaw(string(specs)))
			if !specialtiesOverlap(needSpecs, have) {
				continue
			}
			ordered = append(ordered, oid)
		}
		return ordered, source, nil
	}

	rows, err := s.DB.QueryContext(ctx,
		`SELECT open_id, department, specialties, role_kinds FROM interviewer_profiles WHERE enabled=1`)
	if err != nil {
		return nil, source, err
	}
	defer rows.Close()
	for rows.Next() {
		var oid, dept string
		var specs, kinds []byte
		if err := rows.Scan(&oid, &dept, &specs, &kinds); err != nil {
			continue
		}
		if exclude[oid] {
			continue
		}
		okRole := false
		for _, k := range parseStringSlice(jsonRaw(string(kinds))) {
			if strings.EqualFold(k, role) {
				okRole = true
				break
			}
		}
		if !okRole {
			continue
		}
		if matchDept && jdDept != "" && !strings.EqualFold(dept, jdDept) {
			continue
		}
		have := parseStringSlice(jsonRaw(string(specs)))
		if !specialtiesOverlap(needSpecs, have) {
			continue
		}
		ordered = append(ordered, oid)
	}
	return ordered, source, nil
}

func (s *Service) jdDepartment(ctx context.Context, jdRoundID string) (string, error) {
	var dept string
	err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(j.department,'') FROM jd_interview_rounds r
		 JOIN job_descriptions j ON j.id = r.jd_id WHERE r.id=?`, jdRoundID,
	).Scan(&dept)
	return dept, err
}

// resolveRoleAttendees fills headcount for one requirement using fixed → pool → profile catalog.
func (s *Service) resolveRoleAttendees(ctx context.Context, req map[string]any, jdDept string, exclude map[string]bool) ([]string, map[string]any, error) {
	role, _ := req["role_kind"].(string)
	hc := 1
	switch v := req["headcount"].(type) {
	case int:
		hc = v
	case float64:
		hc = int(v)
	}
	if hc <= 0 {
		hc = 1
	}
	matchDept := false
	switch v := req["match_jd_department"].(type) {
	case bool:
		matchDept = v
	case int:
		matchDept = v == 1
	}
	poolID, _ := req["pool_id"].(string)
	poolID = strings.TrimSpace(poolID)
	needSpecs := parseStringSlice(req["specialties"])
	fixed := cleanOpenIDs(parseStringSlice(req["fixed_open_ids"]))

	var picked []string
	sources := []string{}
	for _, id := range fixed {
		if exclude[id] {
			continue
		}
		picked = append(picked, id)
		exclude[id] = true
		if len(picked) >= hc {
			break
		}
	}
	if len(picked) > 0 {
		sources = append(sources, "fixed")
	}

	if len(picked) < hc {
		cands, src, err := s.candidatesForRole(ctx, role, poolID, jdDept, needSpecs, matchDept, exclude)
		if err != nil {
			return nil, nil, err
		}
		for _, id := range cands {
			if exclude[id] {
				continue
			}
			picked = append(picked, id)
			exclude[id] = true
			if len(picked) >= hc {
				break
			}
		}
		if src != "" {
			sources = append(sources, src)
		}
	}

	if len(picked) < hc {
		return nil, nil, fmt.Errorf("interviewers_unassigned: role %s needs %d, resolved %d (fixed/pool/profiles)", role, hc, len(picked))
	}
	return picked[:hc], map[string]any{
		"role_kind": role, "headcount": hc, "open_ids": picked[:hc], "sources": sources,
	}, nil
}

// ManualScheduleInput lets HR set assignees and/or hand-picked slots then re-invite.
type ManualScheduleInput struct {
	AssignedOpenIDs []string `json:"assigned_open_ids"`
	Slots           []struct {
		StartsAt time.Time `json:"starts_at"`
		EndsAt   time.Time `json:"ends_at"`
		Location string    `json:"location"`
	} `json:"slots"`
	ResendInvite bool `json:"resend_invite"`
}

func (s *Service) ManualScheduleRound(ctx context.Context, appID string, in ManualScheduleInput) error {
	var status string
	var jdID string
	var roundIndex int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT status, jd_id, current_round_index FROM applications WHERE id=?`, appID,
	).Scan(&status, &jdID, &roundIndex); err != nil {
		return err
	}
	if status != db.StatusNeedsHuman && status != db.StatusAwaitingReply && status != db.StatusScreened {
		return fmt.Errorf("cannot manual schedule in status=%s", status)
	}
	appRoundID, _, duration, err := s.EnsureApplicationRound(ctx, appID, jdID, roundIndex)
	if err != nil {
		return err
	}
	attendees := cleanOpenIDs(in.AssignedOpenIDs)
	if len(attendees) == 0 {
		// keep existing assigned
		attendees = s.loadAssignedOpenIDs(ctx, appID)
	}
	if len(attendees) == 0 {
		return fmt.Errorf("assigned_open_ids required")
	}
	assignedJSON, _ := json.Marshal(attendees)
	detailJSON, _ := json.Marshal(map[string]any{"resolver": "hr_manual", "assigned_open_ids": attendees})
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE application_interview_rounds SET assigned_open_ids=?, assignment_detail=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		assignedJSON, detailJSON, appRoundID,
	)

	if len(in.Slots) > 0 {
		_ = s.releaseSoftSlots(ctx, appID)
		for i, sl := range in.Slots {
			if sl.StartsAt.IsZero() || sl.EndsAt.IsZero() || !sl.EndsAt.After(sl.StartsAt) {
				return fmt.Errorf("invalid slot %d", i)
			}
			loc := sl.Location
			if loc == "" {
				loc = "线上会议"
			}
			sid := uuid.NewString()
			st := "proposed"
			proposed := 1
			if len(in.Slots) == 1 {
				st = "held"
				proposed = 0
			}
			_, err := s.DB.ExecContext(ctx,
				`INSERT INTO interview_slots (id, application_id, application_round_id, starts_at, ends_at, location, status, provider_event_id, is_proposed, source)
				 VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, 'hr_manual')`,
				sid, appID, appRoundID, sl.StartsAt, sl.EndsAt, loc, st, proposed,
			)
			if err != nil {
				return err
			}
			_ = duration
		}
	} else if in.ResendInvite {
		// auto regenerate slots with current attendees
		_ = s.releaseSoftSlots(ctx, appID)
		slots, err := s.Calendar.ListSlots(ctx, calendar.Constraints{
			Limit: 3, Duration: duration, AttendeeIDs: attendees,
		})
		if err != nil {
			return err
		}
		if len(slots) == 0 {
			return fmt.Errorf("no calendar slots available")
		}
		for _, sl := range slots {
			_, _ = s.DB.ExecContext(ctx,
				`INSERT INTO interview_slots (id, application_id, application_round_id, starts_at, ends_at, location, status, provider_event_id, is_proposed, source)
				 VALUES (?, ?, ?, ?, ?, ?, 'proposed', '', 1, 'auto')`,
				sl.ID, appID, appRoundID, sl.StartsAt, sl.EndsAt, sl.Location,
			)
		}
	}

	if !in.ResendInvite {
		_ = s.Audit.Log(ctx, audit.Event{
			ApplicationID: appID, Action: "manual_schedule_saved", AfterStatus: status,
			Detail: map[string]any{"attendee_open_ids": attendees, "slots": len(in.Slots), "invite": false},
		})
		return nil
	}

	// send / resend invite email
	var email, name, threadID string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT candidate_email, candidate_name, thread_id FROM applications WHERE id=?`, appID,
	).Scan(&email, &name, &threadID); err != nil {
		return err
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, starts_at, ends_at, location FROM interview_slots
		 WHERE application_id=? AND status IN ('held','proposed') ORDER BY starts_at`, appID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var proposed []calendar.Slot
	for rows.Next() {
		var id, loc string
		var start, end time.Time
		_ = rows.Scan(&id, &start, &end, &loc)
		proposed = append(proposed, calendar.Slot{ID: id, StartsAt: start, EndsAt: end, Location: loc, AttendeeIDs: attendees})
	}
	if len(proposed) == 0 {
		return fmt.Errorf("no slots to invite")
	}
	token, err := replytoken.Issue(s.Cfg.ReplyTokenSecret, appID, s.Cfg.ReplyTimeout())
	if err != nil {
		return err
	}
	replyURL := strings.TrimRight(s.Cfg.PublicBaseURL, "/") + "/r/" + token
	hours := s.Cfg.ReplyTimeoutHours
	if hours <= 0 {
		hours = 48
	}
	subject, body := buildInviteEmail(name, proposed, threadID, replyURL, hours)
	msgID := uuid.NewString()
	idem := "manual-" + msgID[:8]
	if err := s.Mail.Enqueue(ctx, mail.Message{
		ID: msgID, ApplicationID: appID, IdempotencyKey: idem,
		To: email, Subject: subject, Body: body,
	}); err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE applications SET status=?, last_message_id=?, error_message=NULL, human_reason_code=NULL, error_kind='none' WHERE id=?`,
		db.StatusAwaitingReply, msgID, appID,
	)
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE application_interview_rounds SET status='awaiting_reply', updated_at=CURRENT_TIMESTAMP WHERE id=?`, appRoundID)
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID, Action: "manual_invite_enqueued", AfterStatus: db.StatusAwaitingReply,
		Detail: map[string]any{"slots": len(proposed), "attendee_open_ids": attendees, "source": "hr_manual"},
	})
	return err
}
