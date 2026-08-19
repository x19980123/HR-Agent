package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hr-agent/services/internal/agentclient"
	"github.com/hr-agent/services/internal/audit"
	"github.com/hr-agent/services/internal/calendar"
	"github.com/hr-agent/services/internal/config"
	"github.com/hr-agent/services/internal/db"
	"github.com/hr-agent/services/internal/feishucontact"
	"github.com/hr-agent/services/internal/idempotency"
	"github.com/hr-agent/services/internal/mail"
	"github.com/hr-agent/services/internal/replytoken"
)

type Service struct {
	DB            *sql.DB
	Cfg           *config.Config
	Agent         *agentclient.Client
	Calendar      calendar.Provider
	Mail          mail.Sender
	Audit         *audit.Logger
	FeishuContact *feishucontact.Client
}

type StartInput struct {
	JDID           string
	CandidateEmail string
	CandidateName  string
	ResumePath     string
	ResumeText     string
	ContactSource  string
	ExternalID     string
	Channel        string
}

func (s *Service) Start(ctx context.Context, in StartInput) (string, error) {
	appID := uuid.NewString()
	threadID := appID
	ch := nullStr(strings.TrimSpace(in.Channel))
	ext := nullStr(strings.TrimSpace(in.ExternalID))

	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO applications
		 (id, jd_id, channel, external_id, candidate_email, candidate_name, resume_path, status, thread_id, reschedule_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		appID, in.JDID, ch, ext, in.CandidateEmail, in.CandidateName, in.ResumePath, db.StatusUploaded, threadID,
	)
	if err != nil {
		return "", err
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "application_created",
		AfterStatus:   db.StatusUploaded,
		Detail:        map[string]any{"jd_id": in.JDID, "email": in.CandidateEmail},
	})
	s.setContactOnCreate(ctx, appID, in.CandidateEmail, in.ContactSource, 0)

	// Run parse/screen/invite asynchronously so the admin UI can poll live progress.
	go func(appID string, in StartInput) {
		bg := context.Background()
		if err := s.runIntelligentAndSchedule(bg, appID, in); err != nil {
			code := "pipeline_error"
			if isAgentUnavailable(err) {
				code = "agent_unavailable"
			}
			_ = s.markSystemFailed(bg, appID, code, err.Error())
		}
	}(appID, in)

	return appID, nil
}

func (s *Service) resolveResumePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	try := func(p string) string {
		if p == "" {
			return ""
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			if abs, err := filepath.Abs(p); err == nil {
				return abs
			}
			return p
		}
		return ""
	}
	if got := try(path); got != "" {
		return got
	}
	base := filepath.Base(path)
	if s.Cfg != nil && s.Cfg.UploadDir != "" {
		if got := try(filepath.Join(s.Cfg.UploadDir, base)); got != "" {
			return got
		}
	}
	// Legacy relative paths like uploads\uuid.pdf from another working directory.
	if got := try(filepath.Join("uploads", base)); got != "" {
		return got
	}
	if got := try(filepath.Join("services", "uploads", base)); got != "" {
		return got
	}
	return path
}

func (s *Service) runIntelligentAndSchedule(ctx context.Context, appID string, in StartInput) error {
	_, _ = s.DB.ExecContext(ctx, `UPDATE applications SET status=? WHERE id=?`, db.StatusParsing, appID)
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "parse_started",
		AfterStatus:   db.StatusParsing,
	})

	jd, err := s.loadJD(ctx, in.JDID)
	if err != nil {
		return err
	}

	resumePath := s.resolveResumePath(in.ResumePath)
	if resumePath != "" && resumePath != in.ResumePath {
		_, _ = s.DB.ExecContext(ctx, `UPDATE applications SET resume_path=? WHERE id=?`, resumePath, appID)
		in.ResumePath = resumePath
	}

	resp, err := s.Agent.RunParseScreen(ctx, agentclient.PipelineRequest{
		ApplicationID: appID,
		ResumePath:    in.ResumePath,
		ResumeText:    in.ResumeText,
		JD:            jd,
	})
	if err != nil {
		return fmt.Errorf("agent parse_screen: %w", err)
	}

	s.applyProfileContact(ctx, appID, resp.Profile)

	var preservedHumanCode sql.NullString
	var curStatus string
	_ = s.DB.QueryRowContext(ctx, `SELECT status, human_reason_code FROM applications WHERE id=?`, appID).Scan(&curStatus, &preservedHumanCode)

	if curStatus != db.StatusNeedsHuman {
		_, _ = s.DB.ExecContext(ctx, `UPDATE applications SET status=? WHERE id=?`, db.StatusScreening, appID)
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "screen_started",
		AfterStatus:   db.StatusScreening,
	})

	profileJSON, _ := json.Marshal(resp.Profile)
	screenJSON, _ := json.Marshal(resp.Screen)

	// Parse failure / empty profile must not be treated as screen reject.
	if curStatus == db.StatusNeedsHuman && preservedHumanCode.Valid && preservedHumanCode.String == "contact_csv_parse_mismatch" {
		profileJSON, _ := json.Marshal(resp.Profile)
		screenJSON, _ := json.Marshal(resp.Screen)
		_, err = s.DB.ExecContext(ctx,
			`UPDATE applications SET profile_json=?, screen_json=?, langsmith_run_id=? WHERE id=?`,
			profileJSON, screenJSON, nullStr(resp.LangsmithRunID), appID)
		return err
	}
	if resp.NeedsHuman {
		reason := strings.TrimSpace(resp.Error)
		if reason == "" {
			reason = "解析/筛选需人工处理"
		}
		humanCode := strings.TrimSpace(resp.HumanReasonCode)
		if humanCode == "" {
			humanCode = "needs_human"
		}
		if preservedHumanCode.Valid && preservedHumanCode.String == "contact_csv_parse_mismatch" {
			humanCode = preservedHumanCode.String
			reason = "CSV 映射邮箱与解析结果不一致，请 HR 确认联系方式"
		}
		action := "needs_human_after_parse"
		if strings.HasPrefix(humanCode, "screen_") {
			action = "needs_human_after_screen"
		}
		_, err = s.DB.ExecContext(ctx,
			`UPDATE applications SET status=?, profile_json=?, screen_json=?, langsmith_run_id=?, error_message=?,
			 error_kind=?, human_reason_code=?, system_error_code=NULL, screen_tier=? WHERE id=?`,
			db.StatusNeedsHuman, profileJSON, screenJSON, nullStr(resp.LangsmithRunID), reason,
			ErrorKindBusiness, humanCode, nullStr(resp.ScreenTier), appID)
		_ = s.Audit.Log(ctx, audit.Event{
			ApplicationID:  appID,
			Action:         action,
			AfterStatus:    db.StatusNeedsHuman,
			LangsmithRunID: resp.LangsmithRunID,
			Detail:         map[string]any{"reason": reason, "human_reason_code": humanCode, "screen_tier": resp.ScreenTier},
		})
		return err
	}
	if resp.Rejected {
		_, err = s.DB.ExecContext(ctx,
			`UPDATE applications SET status=?, profile_json=?, screen_json=?, langsmith_run_id=?, error_message=NULL,
			 error_kind=?, human_reason_code=NULL, system_error_code=NULL, screen_tier=? WHERE id=?`,
			db.StatusRejected, profileJSON, screenJSON, nullStr(resp.LangsmithRunID),
			ErrorKindBusiness, nullStr(resp.ScreenTier), appID)
		_ = s.Audit.Log(ctx, audit.Event{ApplicationID: appID, Action: "rejected_by_screen", AfterStatus: db.StatusRejected, LangsmithRunID: resp.LangsmithRunID})
		return err
	}

	_, err = s.DB.ExecContext(ctx,
		`UPDATE applications SET status=?, profile_json=?, screen_json=?, langsmith_run_id=?, error_message=NULL,
		 error_kind=?, human_reason_code=NULL, system_error_code=NULL, screen_tier=? WHERE id=?`,
		db.StatusScreened, profileJSON, screenJSON, nullStr(resp.LangsmithRunID),
		ErrorKindNone, nullStr(resp.ScreenTier), appID)
	if err != nil {
		return err
	}
	_ = s.Audit.Log(ctx, audit.Event{ApplicationID: appID, Action: "screened", AfterStatus: db.StatusScreened, LangsmithRunID: resp.LangsmithRunID})
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "invite_preparing",
		AfterStatus:   db.StatusScreened,
	})

	if err := s.contactGate(ctx, appID); err != nil {
		return err
	}
	return s.scheduleAndNotify(ctx, appID, false, nil)
}

func (s *Service) releaseSoftSlots(ctx context.Context, appID string) error {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT provider_event_id FROM interview_slots WHERE application_id=? AND status IN ('held','proposed')`, appID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var eid sql.NullString
		_ = rows.Scan(&eid)
		if eid.Valid && eid.String != "" {
			_ = s.Calendar.Release(ctx, eid.String)
		}
	}
	rows.Close()
	_, _ = s.DB.ExecContext(ctx, `UPDATE interview_slots SET status='released' WHERE application_id=? AND status IN ('held','proposed')`, appID)
	return nil
}

// refreshProposedSlots replaces proposed times in-page (no second email).
func (s *Service) refreshProposedSlots(ctx context.Context, appID string, preferred []string) error {
	var rescheduleCount int
	if err := s.DB.QueryRowContext(ctx, `SELECT reschedule_count FROM applications WHERE id=?`, appID).Scan(&rescheduleCount); err != nil {
		return err
	}
	if err := s.releaseSoftSlots(ctx, appID); err != nil {
		return err
	}
	attendees, duration, appRoundID, prepErr := s.prepareRoundScheduling(ctx, appID)
	if prepErr != nil {
		return s.markHumanWithCode(ctx, appID, prepErr.Error(), schedulingHumanCode(prepErr))
	}
	slots, err := s.Calendar.ListSlots(ctx, calendar.Constraints{
		PreferredWindows: preferred,
		Limit:            3,
		Duration:         duration,
		AttendeeIDs:      attendees,
	})
	if err != nil {
		return err
	}
	if len(slots) == 0 {
		return s.markHuman(ctx, appID, "no_calendar_slots_for_reschedule")
	}
	for _, sl := range slots {
		_, _ = s.DB.ExecContext(ctx,
			`INSERT INTO interview_slots (id, application_id, application_round_id, starts_at, ends_at, location, status, provider_event_id, is_proposed, source)
			 VALUES (?, ?, ?, ?, ?, ?, 'proposed', '', 1, 'auto')`,
			sl.ID, appID, nullStr(appRoundID), sl.StartsAt, sl.EndsAt, sl.Location,
		)
	}
	newCount := rescheduleCount + 1
	_, err = s.DB.ExecContext(ctx,
		`UPDATE applications SET status=?, reschedule_count=? WHERE id=?`,
		db.StatusAwaitingReply, newCount, appID,
	)
	if err != nil {
		return err
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "reschedule_slots_refreshed",
		AfterStatus:   db.StatusAwaitingReply,
		Detail:        map[string]any{"slots": len(slots), "reschedule_count": newCount, "email": false},
	})
	return nil
}

// prepareRoundScheduling ensures current application round exists, resolves attendees
// (Scheduling Agent when enabled, else Phase 2 classified_profiles).
func (s *Service) prepareRoundScheduling(ctx context.Context, appID string) (attendees []string, duration time.Duration, appRoundID string, err error) {
	var jdID string
	var roundIndex int
	if err = s.DB.QueryRowContext(ctx,
		`SELECT jd_id, current_round_index FROM applications WHERE id=?`, appID,
	).Scan(&jdID, &roundIndex); err != nil {
		return nil, 0, "", err
	}
	n, err := s.CountJDRounds(ctx, jdID)
	if err != nil {
		return nil, 0, "", err
	}
	if n == 0 {
		return nil, 0, "", fmt.Errorf("interview_plan_missing: 请先在 JD 配置面试轮次与面试官")
	}
	appRoundID, jdRoundID, duration, err := s.EnsureApplicationRound(ctx, appID, jdID, roundIndex)
	if err != nil {
		return nil, 0, "", err
	}

	var detail map[string]any
	if s.Cfg != nil && s.Cfg.SchedulingAgentEnabled {
		attendees, detail, err = s.resolveAttendeesWithAgent(ctx, appID, jdRoundID, roundIndex, duration)
	} else {
		attendees, detail, err = s.ResolveAttendeesForRound(ctx, jdRoundID)
	}
	if detail == nil {
		detail = map[string]any{}
	}
	if err != nil {
		detailJSON, _ := json.Marshal(detail)
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE application_interview_rounds SET assignment_detail=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
			detailJSON, appRoundID,
		)
		return nil, 0, appRoundID, err
	}
	assignedJSON, _ := json.Marshal(attendees)
	detailJSON, _ := json.Marshal(detail)
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE application_interview_rounds SET assigned_open_ids=?, assignment_detail=?, status='pending', updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		assignedJSON, detailJSON, appRoundID,
	)
	return attendees, duration, appRoundID, nil
}

// scheduleAndNotify sends the first invite email only (no Feishu event yet).
func (s *Service) scheduleAndNotify(ctx context.Context, appID string, isReschedule bool, preferred []string) error {
	if isReschedule {
		return s.refreshProposedSlots(ctx, appID, preferred)
	}
	if err := s.contactGate(ctx, appID); err != nil {
		return err
	}

	var email, name, threadID string
	var roundIndex int
	err := s.DB.QueryRowContext(ctx,
		`SELECT candidate_email, candidate_name, thread_id, current_round_index FROM applications WHERE id=?`, appID,
	).Scan(&email, &name, &threadID, &roundIndex)
	if err != nil {
		return err
	}

	attendees, duration, appRoundID, prepErr := s.prepareRoundScheduling(ctx, appID)
	if prepErr != nil {
		return s.markHumanWithCode(ctx, appID, prepErr.Error(), schedulingHumanCode(prepErr))
	}

	slots, err := s.Calendar.ListSlots(ctx, calendar.Constraints{
		PreferredWindows: preferred,
		Limit:            1,
		Duration:         duration,
		AttendeeIDs:      attendees,
	})
	if err != nil {
		return err
	}
	if len(slots) == 0 {
		return s.markHumanWithCode(ctx, appID, "no calendar slots available", "no_calendar_slots")
	}

	sl := slots[0]
	sl.AttendeeIDs = attendees
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO interview_slots (id, application_id, application_round_id, starts_at, ends_at, location, status, provider_event_id, is_proposed, source)
		 VALUES (?, ?, ?, ?, ?, ?, 'held', '', 0, 'auto')`,
		sl.ID, appID, appRoundID, sl.StartsAt, sl.EndsAt, sl.Location,
	)
	if err != nil {
		return err
	}
	proposed := []calendar.Slot{sl}

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
	idem := idempotency.Key(appID, "notify", proposed[0].ID)
	msgID := uuid.NewString()
	if err := s.Mail.Enqueue(ctx, mail.Message{
		ID:             msgID,
		ApplicationID:  appID,
		IdempotencyKey: idem,
		To:             email,
		Subject:        subject,
		Body:           body,
	}); err != nil {
		return err
	}

	_, err = s.DB.ExecContext(ctx,
		`UPDATE applications SET status=?, last_message_id=? WHERE id=?`,
		db.StatusAwaitingReply, msgID, appID,
	)
	if err != nil {
		return err
	}
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE application_interview_rounds SET status='awaiting_reply', updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		appRoundID,
	)
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID:  appID,
		Action:         "invite_enqueued",
		AfterStatus:    db.StatusAwaitingReply,
		IdempotencyKey: idem,
		Detail: map[string]any{
			"slots": len(proposed), "round_index": roundIndex,
			"attendee_open_ids": attendees, "application_round_id": appRoundID,
		},
	})
	return nil
}

// HandleReply no longer runs classify Agent. Candidates should use /r/{token};
// free-text email replies are escalated to HR for manual slot selection.
func (s *Service) HandleReply(ctx context.Context, appID, emailBody string) error {
	var status string
	err := s.DB.QueryRowContext(ctx, `SELECT status FROM applications WHERE id=?`, appID).Scan(&status)
	if err != nil {
		return err
	}
	if status != db.StatusAwaitingReply {
		return fmt.Errorf("application %s not awaiting reply (status=%s)", appID, status)
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "email_reply_needs_human",
		AfterStatus:   db.StatusNeedsHuman,
		Detail:        map[string]any{"hint": "use_public_reply_link", "body_preview": truncateRunes(emailBody, 200)},
	})
	return s.markHumanWithCode(ctx, appID, "请使用邮件中的链接选择时段；或由 HR 在管理台手工排期", "email_reply_use_link")
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (s *Service) confirm(ctx context.Context, appID string, selectedIdx *int) error {
	query := `SELECT id, provider_event_id, starts_at, ends_at, location FROM interview_slots
		 WHERE application_id=? AND status IN ('held','proposed') ORDER BY starts_at`
	rows, err := s.DB.QueryContext(ctx, query, appID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type sl struct {
		id, eid, loc string
		start, end   time.Time
	}
	var list []sl
	for rows.Next() {
		var id, loc string
		var eid sql.NullString
		var start, end time.Time
		_ = rows.Scan(&id, &eid, &start, &end, &loc)
		list = append(list, sl{id: id, eid: eid.String, loc: loc, start: start, end: end})
	}
	if len(list) == 0 {
		return s.markHuman(ctx, appID, "no_slot_to_confirm")
	}
	idx := 0
	if selectedIdx != nil && *selectedIdx >= 0 && *selectedIdx < len(list) {
		idx = *selectedIdx
	}
	chosen := list[idx]

	idemKey := idempotency.Key(appID, "confirm", chosen.id)
	acquired, idemErr := idempotency.Acquire(ctx, s.DB, idemKey, "confirm", appID)
	if idemErr != nil {
		return idemErr
	}
	if !acquired {
		var st string
		_ = s.DB.QueryRowContext(ctx, `SELECT status FROM applications WHERE id=?`, appID).Scan(&st)
		if st == db.StatusConfirmed {
			return nil
		}
		return fmt.Errorf("confirm already processed")
	}

	attendees := s.loadAssignedOpenIDs(ctx, appID)
	book := calendar.BookResult{EventID: chosen.eid, Location: chosen.loc}
	// Create Feishu/calendar event only after the candidate confirms.
	if chosen.eid == "" {
		res, holdErr := s.Calendar.Hold(ctx, calendar.Slot{
			ID:          chosen.id,
			StartsAt:    chosen.start,
			EndsAt:      chosen.end,
			Location:    chosen.loc,
			AttendeeIDs: attendees,
		}, appID)
		if holdErr != nil {
			return fmt.Errorf("create calendar event on confirm: %w", holdErr)
		}
		book = res
		chosen.eid = res.EventID
		_, _ = s.DB.ExecContext(ctx, `UPDATE interview_slots SET provider_event_id=?, meeting_url=? WHERE id=?`,
			res.EventID, nullStr(res.MeetingURL), chosen.id)
	} else if confErr := s.Calendar.Confirm(ctx, chosen.eid); confErr != nil {
		return confErr
	}
	for i, item := range list {
		st := "released"
		if i == idx {
			st = "confirmed"
		} else if item.eid != "" {
			_ = s.Calendar.Release(ctx, item.eid)
		}
		_, _ = s.DB.ExecContext(ctx, `UPDATE interview_slots SET status=? WHERE id=?`, st, item.id)
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE applications SET status=?, reply_intent=? WHERE id=?`, db.StatusConfirmed, "accept", appID)
	var curRound int
	_ = s.DB.QueryRowContext(ctx, `SELECT current_round_index FROM applications WHERE id=?`, appID).Scan(&curRound)
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE application_interview_rounds SET status='confirmed', confirmed_slot_id=?, provider_event_id=?, updated_at=CURRENT_TIMESTAMP
		 WHERE application_id=? AND round_index=?`,
		chosen.id, chosen.eid, appID, curRound,
	)
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "interview_confirmed",
		AfterStatus:   db.StatusConfirmed,
		Detail: map[string]any{
			"slot_id": chosen.id, "provider_event_id": chosen.eid, "meeting_url": book.MeetingURL,
			"attendee_open_ids": attendees,
		},
	})
	if err != nil {
		return err
	}

	if mailErr := s.enqueueConfirmEmail(ctx, appID, chosen.start, chosen.end, book); mailErr != nil {
		_ = s.Audit.Log(ctx, audit.Event{
			ApplicationID: appID,
			Action:        "confirm_email_failed",
			AfterStatus:   db.StatusConfirmed,
			Detail:        map[string]any{"error": mailErr.Error()},
		})
	}

	// Questions are generated only after the candidate confirms the interview time (HR-facing).
	if qErr := s.generateQuestionsAfterConfirm(ctx, appID); qErr != nil {
		_, _ = s.DB.ExecContext(ctx, `UPDATE applications SET error_message=? WHERE id=?`,
			"questions_failed: "+qErr.Error(), appID)
		_ = s.Audit.Log(ctx, audit.Event{
			ApplicationID: appID,
			Action:        "questions_failed",
			AfterStatus:   db.StatusConfirmed,
			Detail:        map[string]any{"error": qErr.Error()},
		})
	}
	return nil
}

func (s *Service) generateQuestionsAfterConfirm(ctx context.Context, appID string) error {
	var jdID string
	var profileJSON sql.NullString
	if err := s.DB.QueryRowContext(ctx,
		`SELECT jd_id, profile_json FROM applications WHERE id=?`, appID,
	).Scan(&jdID, &profileJSON); err != nil {
		return err
	}
	if !profileJSON.Valid || profileJSON.String == "" {
		return fmt.Errorf("missing profile")
	}
	var profile map[string]any
	if err := json.Unmarshal([]byte(profileJSON.String), &profile); err != nil {
		return err
	}
	jd, err := s.loadJD(ctx, jdID)
	if err != nil {
		return err
	}
	hints, _ := s.buildQuestionBankHints(ctx, profile, jd)
	resp, err := s.Agent.GenerateQuestions(ctx, agentclient.QuestionsRequest{
		ApplicationID: appID,
		Profile:       profile,
		JD:            jd,
		BankHints:     hints,
	})
	if err != nil {
		return err
	}
	qJSON, _ := json.Marshal(resp.Questions)
	if resp.LangsmithRunID != "" {
		_, err = s.DB.ExecContext(ctx,
			`UPDATE applications SET questions_json=?, langsmith_run_id=?, error_message=NULL WHERE id=?`,
			qJSON, resp.LangsmithRunID, appID)
	} else {
		_, err = s.DB.ExecContext(ctx,
			`UPDATE applications SET questions_json=?, error_message=NULL WHERE id=?`,
			qJSON, appID)
	}
	if err != nil {
		return err
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID:  appID,
		Action:         "questions_generated",
		AfterStatus:    db.StatusConfirmed,
		LangsmithRunID: resp.LangsmithRunID,
		Detail:         map[string]any{"count": len(resp.Questions)},
	})
	if packErr := s.DispatchInterviewerPackEmails(ctx, appID); packErr != nil {
		_ = s.Audit.Log(ctx, audit.Event{
			ApplicationID: appID,
			Action:        "interviewer_pack_email_failed",
			AfterStatus:   db.StatusConfirmed,
			Detail:        map[string]any{"error": packErr.Error()},
		})
	}
	return nil
}

func (s *Service) decline(ctx context.Context, appID string) error {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT provider_event_id FROM interview_slots WHERE application_id=? AND status IN ('held','proposed')`, appID)
	if err == nil {
		for rows.Next() {
			var eid sql.NullString
			_ = rows.Scan(&eid)
			if eid.Valid {
				_ = s.Calendar.Release(ctx, eid.String)
			}
		}
		rows.Close()
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE interview_slots SET status='cancelled' WHERE application_id=? AND status IN ('held','proposed')`, appID)
	_, err = s.DB.ExecContext(ctx, `UPDATE applications SET status=?, reply_intent=? WHERE id=?`, db.StatusDeclined, "decline", appID)
	_ = s.Audit.Log(ctx, audit.Event{ApplicationID: appID, Action: "interview_declined", AfterStatus: db.StatusDeclined})
	return err
}

func (s *Service) markHuman(ctx context.Context, appID, reason string) error {
	return s.markHumanWithCode(ctx, appID, reason, reason)
}

func (s *Service) HumanApprove(ctx context.Context, appID string) error {
	var status, errorKind string
	if err := s.DB.QueryRowContext(ctx, `SELECT status, COALESCE(error_kind,'none') FROM applications WHERE id=?`, appID).Scan(&status, &errorKind); err != nil {
		return err
	}
	if errorKind == ErrorKindSystem {
		return fmt.Errorf("系统故障需联系管理员处理，无法人工通过")
	}
	allowed := status == db.StatusNeedsHuman ||
		status == db.StatusQuestionsReady ||
		status == db.StatusScreened ||
		status == db.StatusRejected ||
		status == db.StatusFailed
	if !allowed {
		return fmt.Errorf("cannot approve status=%s", status)
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE applications SET error_message=NULL WHERE id=?`, appID)
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "human_approved",
		BeforeStatus:  status,
		AfterStatus:   status,
		Detail:        map[string]any{"from_status": status},
	})
	return s.scheduleAndNotify(ctx, appID, false, nil)
}

// RetryParse re-runs parse+screen for applications stuck in human/reject/failed.
func (s *Service) RetryParse(ctx context.Context, appID string, resumeText string) error {
	var status, jdID, resumePath, email, name string
	err := s.DB.QueryRowContext(ctx,
		`SELECT status, jd_id, resume_path, candidate_email, candidate_name FROM applications WHERE id=?`, appID,
	).Scan(&status, &jdID, &resumePath, &email, &name)
	if err != nil {
		return err
	}
	switch status {
	case db.StatusNeedsHuman, db.StatusRejected, db.StatusFailed, db.StatusUploaded, db.StatusParsing:
	default:
		return fmt.Errorf("cannot retry parse status=%s", status)
	}
	if strings.TrimSpace(resumePath) == "" && strings.TrimSpace(resumeText) == "" {
		return fmt.Errorf("no resume file or text to parse")
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "retry_parse",
		BeforeStatus:  status,
		AfterStatus:   db.StatusParsing,
	})
	return s.runIntelligentAndSchedule(ctx, appID, StartInput{
		JDID:           jdID,
		CandidateEmail: email,
		CandidateName:  name,
		ResumePath:     resumePath,
		ResumeText:     resumeText,
	})
}

func (s *Service) SweepTimeouts(ctx context.Context) (int, error) {
	cutoff := time.Now().Add(-s.Cfg.ReplyTimeout())
	res, err := s.DB.ExecContext(ctx,
		`UPDATE applications SET status=?, error_message='reply_timeout'
		 WHERE status=? AND updated_at < ?`,
		db.StatusNeedsHuman, db.StatusAwaitingReply, cutoff,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *Service) loadAssignedOpenIDs(ctx context.Context, appID string) []string {
	var raw []byte
	err := s.DB.QueryRowContext(ctx,
		`SELECT air.assigned_open_ids FROM application_interview_rounds air
		 JOIN applications a ON a.id = air.application_id AND air.round_index = a.current_round_index
		 WHERE a.id=?`, appID,
	).Scan(&raw)
	if err != nil || len(raw) == 0 {
		return nil
	}
	var ids []string
	_ = json.Unmarshal(raw, &ids)
	return cleanOpenIDs(ids)
}

func (s *Service) GetApplication(ctx context.Context, appID string) (map[string]any, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, jd_id, candidate_email, candidate_name, resume_path, status, thread_id, reschedule_count,
		        current_round_index,
		        COALESCE(offer_status,'none'), COALESCE(offer_note,''), offer_updated_at, hired_at,
		        profile_json, screen_json, questions_json, reply_intent, error_message,
		        COALESCE(error_kind,'none'), human_reason_code, system_error_code, screen_tier,
		        contact_email_source, contact_email_confidence, contact_resolved_at,
		        created_at, updated_at
		 FROM applications WHERE id=?`, appID)
	var id, jd, email, name, resumePath, status, thread string
	var rescheduleCount, currentRound int
	var offerStatus, offerNote string
	var offerUpdated, hiredAt sql.NullTime
	var profile, screen, questions, intent, errmsg sql.NullString
	var errorKind, humanCode, systemCode, screenTier sql.NullString
	var contactSource sql.NullString
	var contactConf sql.NullFloat64
	var contactResolved sql.NullTime
	var created, updated time.Time
	if err := row.Scan(&id, &jd, &email, &name, &resumePath, &status, &thread, &rescheduleCount,
		&currentRound,
		&offerStatus, &offerNote, &offerUpdated, &hiredAt,
		&profile, &screen, &questions, &intent, &errmsg,
		&errorKind, &humanCode, &systemCode, &screenTier,
		&contactSource, &contactConf, &contactResolved,
		&created, &updated); err != nil {
		return nil, err
	}
	out := map[string]any{
		"id": id, "jd_id": jd, "candidate_email": email, "candidate_name": name,
		"status": status, "thread_id": thread, "reschedule_count": rescheduleCount,
		"current_round_index": currentRound,
		"offer_status": offerStatus, "offer_note": offerNote,
		"created_at": created, "updated_at": updated,
		"resume_path": resumePath, "resume_name": filepath.Base(resumePath),
		"has_resume": resumePath != "",
		"error_kind": errorKind.String,
	}
	if offerUpdated.Valid {
		out["offer_updated_at"] = offerUpdated.Time
	}
	if hiredAt.Valid {
		out["hired_at"] = hiredAt.Time
	}
	if rounds, rerr := s.ListApplicationRounds(ctx, appID); rerr == nil {
		out["interview_rounds"] = rounds
	}
	if contactSource.Valid {
		out["contact_email_source"] = contactSource.String
	}
	if contactConf.Valid {
		out["contact_email_confidence"] = contactConf.Float64
	}
	if contactResolved.Valid {
		out["contact_resolved_at"] = contactResolved.Time
	}
	if humanCode.Valid {
		out["human_reason_code"] = humanCode.String
	}
	if systemCode.Valid {
		out["system_error_code"] = systemCode.String
	}
	if screenTier.Valid {
		out["screen_tier"] = screenTier.String
	}
	if profile.Valid {
		out["profile"] = jsonRaw(profile.String)
	}
	if screen.Valid {
		out["screen"] = jsonRaw(screen.String)
	}
	if questions.Valid {
		out["questions"] = jsonRaw(questions.String)
	}
	if intent.Valid {
		out["reply_intent"] = intent.String
	}
	if errmsg.Valid {
		out["error_message"] = errmsg.String
	}
	return out, nil
}

func isAgentUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "agent") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "actively refused") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "health")
}

func (s *Service) loadJD(ctx context.Context, jdID string) (map[string]any, error) {
	var title, dept string
	var reqJSON, weightJSON []byte
	err := s.DB.QueryRowContext(ctx,
		`SELECT title, department, requirements, weight_json FROM job_descriptions WHERE id=?`, jdID,
	).Scan(&title, &dept, &reqJSON, &weightJSON)
	if err != nil {
		return nil, fmt.Errorf("load jd: %w", err)
	}
	var req, weight any
	_ = json.Unmarshal(reqJSON, &req)
	_ = json.Unmarshal(weightJSON, &weight)
	return map[string]any{
		"id": jdID, "title": title, "department": dept,
		"requirements": req, "weights": weight,
	}, nil
}

func buildInviteEmail(name string, slots []calendar.Slot, threadID, replyURL string, replyHours int) (string, string) {
	if name == "" {
		name = "候选人"
	}
	if replyHours <= 0 {
		replyHours = 48
	}
	sl := slots[0]
	body := fmt.Sprintf("%s 您好，\n\n邀请您参加面试：\n时间：%s - %s\n形式/地点：%s\n\n请在 %d 小时内点击下方链接操作（可接受、改选其他时段或拒绝）。改期无需等待新邮件，在同一链接内即可完成：\n%s\n\n[thread:%s]\n",
		name,
		sl.StartsAt.Format("2006-01-02 15:04"),
		sl.EndsAt.Format("15:04"),
		sl.Location,
		replyHours,
		replyURL,
		threadID,
	)
	return "面试邀请", body
}

func (s *Service) enqueueConfirmEmail(ctx context.Context, appID string, start, end time.Time, book calendar.BookResult) error {
	var email, name, threadID string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT candidate_email, candidate_name, thread_id FROM applications WHERE id=?`, appID,
	).Scan(&email, &name, &threadID); err != nil {
		return err
	}
	if name == "" {
		name = "候选人"
	}
	loc := book.Location
	if loc == "" {
		loc = s.Cfg.FeishuLocation
	}
	online := book.MeetingURL != "" ||
		strings.Contains(loc, "线上") ||
		strings.Contains(loc, "飞书") ||
		strings.Contains(strings.ToLower(loc), "meeting") ||
		strings.Contains(loc, "会议")

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s 您好，\n\n您已确认面试安排：\n时间：%s - %s\n",
		name, start.Format("2006-01-02 15:04"), end.Format("15:04")))
	if online {
		b.WriteString("形式：线上面试\n")
		if book.MeetingURL != "" {
			b.WriteString(fmt.Sprintf("会议链接：%s\n", book.MeetingURL))
		} else {
			b.WriteString(fmt.Sprintf("会议信息：%s\n（若邮件未附带入会链接，请以飞书日历邀请中的视频会议入口为准）\n", loc))
		}
	} else {
		b.WriteString(fmt.Sprintf("形式：线下面试\n地址/地点：%s\n请提前 10–15 分钟到达，并携带简历与身份证件。\n", loc))
	}
	b.WriteString(fmt.Sprintf("\n如需变更请尽快联系 HR。\n[thread:%s]\n", threadID))

	idem := idempotency.Key(appID, "confirm", book.EventID)
	if book.EventID == "" {
		idem = idempotency.Key(appID, "confirm", start.Format(time.RFC3339))
	}
	return s.Mail.Enqueue(ctx, mail.Message{
		ID:             uuid.NewString(),
		ApplicationID:  appID,
		IdempotencyKey: idem,
		To:             email,
		Subject:        "面试确认函",
		Body:           b.String(),
	})
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func jsonRaw(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil
	}
	return v
}
