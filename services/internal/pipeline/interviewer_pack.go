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
	"github.com/hr-agent/services/internal/mail"
	"github.com/hr-agent/services/internal/replytoken"
)

type InterviewerPackLink struct {
	OpenID    string    `json:"open_id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type PublicInterviewerPackView struct {
	ApplicationID  string           `json:"application_id"`
	CandidateName  string           `json:"candidate_name"`
	JDTitle        string           `json:"jd_title"`
	RoundIndex     int              `json:"round_index"`
	RoundName      string           `json:"round_name"`
	RoundTheme     string           `json:"round_theme,omitempty"`
	InterviewStart time.Time        `json:"interview_start,omitempty"`
	InterviewEnd   time.Time        `json:"interview_end,omitempty"`
	Location       string           `json:"location,omitempty"`
	ExpiresAt      time.Time        `json:"expires_at"`
	PackHours      int              `json:"pack_hours"`
	InterviewerName string          `json:"interviewer_name,omitempty"`
	Questions      []map[string]any `json:"questions"`
}

func (s *Service) BuildInterviewerPackLinks(ctx context.Context, appID string) ([]InterviewerPackLink, error) {
	var status string
	var roundIdx int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT status, current_round_index FROM applications WHERE id=?`, appID,
	).Scan(&status, &roundIdx); err != nil {
		return nil, err
	}
	if status != "confirmed" {
		return nil, fmt.Errorf("application not confirmed")
	}
	assigned := s.loadAssignedOpenIDs(ctx, appID)
	if len(assigned) == 0 {
		return nil, fmt.Errorf("no assigned interviewers for current round")
	}
	base := strings.TrimRight(s.Cfg.PublicBaseURL, "/")
	ttl := s.Cfg.InterviewerPackTimeout()
	exp := time.Now().Add(ttl)
	hours := s.Cfg.InterviewerPackHours
	if hours <= 0 {
		hours = 24
	}
	out := make([]InterviewerPackLink, 0, len(assigned))
	for _, oid := range assigned {
		name, email := s.lookupInterviewerContact(ctx, oid)
		tok, err := replytoken.IssueInterviewerPack(s.Cfg.ReplyTokenSecret, appID, oid, roundIdx, ttl)
		if err != nil {
			continue
		}
		out = append(out, InterviewerPackLink{
			OpenID: oid, Name: name, Email: email,
			URL: base + "/i/" + tok, ExpiresAt: exp,
		})
	}
	return out, nil
}

func (s *Service) DispatchInterviewerPackEmails(ctx context.Context, appID string) error {
	links, err := s.BuildInterviewerPackLinks(ctx, appID)
	if err != nil {
		return err
	}
	var candName, threadID string
	_ = s.DB.QueryRowContext(ctx,
		`SELECT candidate_name, thread_id FROM applications WHERE id=?`, appID,
	).Scan(&candName, &threadID)
	if candName == "" {
		candName = "候选人"
	}
	hours := s.Cfg.InterviewerPackHours
	if hours <= 0 {
		hours = 24
	}
	var firstErr error
	for _, lk := range links {
		if strings.TrimSpace(lk.Email) == "" {
			_ = s.Audit.Log(ctx, audit.Event{
				ApplicationID: appID,
				Action:        "interviewer_pack_skipped_no_email",
				Detail:        map[string]any{"open_id": lk.OpenID, "name": lk.Name},
			})
			continue
		}
		subject, body := buildInterviewerPackEmail(lk.Name, candName, lk.URL, hours)
		msgID := uuid.NewString()
		if err := s.Mail.Enqueue(ctx, mail.Message{
			ID: msgID, ApplicationID: appID,
			IdempotencyKey: "interviewer-pack:" + appID + ":" + lk.OpenID,
			To: lk.Email, Subject: subject, Body: body,
		}); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		_ = s.Audit.Log(ctx, audit.Event{
			ApplicationID: appID,
			Action:        "interviewer_pack_sent",
			Detail: map[string]any{
				"open_id": lk.OpenID, "email": lk.Email, "url": lk.URL,
				"expires_at": lk.ExpiresAt,
			},
		})
	}
	return firstErr
}

func buildInterviewerPackEmail(interviewerName, candidateName, packURL string, packHours int) (string, string) {
	if interviewerName == "" {
		interviewerName = "面试官"
	}
	if candidateName == "" {
		candidateName = "候选人"
	}
	if packHours <= 0 {
		packHours = 24
	}
	body := fmt.Sprintf(
		"%s 您好，\n\n您即将面试的候选人：%s\n\n请在本场面试前打开以下链接查看面试题与参考答案（限时 %d 小时，请勿转发）：\n%s\n\n[thread:%s]\n",
		interviewerName, candidateName, packHours, packURL, candidateName,
	)
	return "面试题单 · " + candidateName, body
}

func (s *Service) PublicInterviewerPackView(ctx context.Context, token string) (*PublicInterviewerPackView, error) {
	claims, err := replytoken.Verify(s.Cfg.ReplyTokenSecret, token)
	if err != nil {
		return nil, err
	}
	if claims.Purpose != replytoken.PurposeInterviewerPack || strings.TrimSpace(claims.OpenID) == "" {
		return nil, replytoken.ErrInvalid
	}

	var candName, status string
	var jdID string
	var roundIdx int
	var questionsRaw sql.NullString
	if err := s.DB.QueryRowContext(ctx,
		`SELECT candidate_name, status, jd_id, current_round_index, questions_json FROM applications WHERE id=?`,
		claims.AppID,
	).Scan(&candName, &status, &jdID, &roundIdx, &questionsRaw); err != nil {
		return nil, fmt.Errorf("application not found")
	}
	if status != "confirmed" {
		return nil, fmt.Errorf("interview pack not available")
	}
	if roundIdx != claims.RoundIndex {
		return nil, replytoken.ErrInvalid
	}
	assigned := s.loadAssignedOpenIDs(ctx, claims.AppID)
	if !openIDInList(claims.OpenID, assigned) {
		return nil, replytoken.ErrInvalid
	}

	jdTitle := ""
	_ = s.DB.QueryRowContext(ctx, `SELECT title FROM job_descriptions WHERE id=?`, jdID).Scan(&jdTitle)

	roundName, roundTheme := "", ""
	var slotID sql.NullString
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(jdr.name,''), COALESCE(jdr.theme,''), air.confirmed_slot_id
		 FROM application_interview_rounds air
		 LEFT JOIN jd_interview_rounds jdr ON jdr.id = air.jd_round_id
		 WHERE air.application_id=? AND air.round_index=?`,
		claims.AppID, claims.RoundIndex,
	).Scan(&roundName, &roundTheme, &slotID)

	var start, end time.Time
	loc := ""
	if slotID.Valid && slotID.String != "" {
		_ = s.DB.QueryRowContext(ctx,
			`SELECT starts_at, ends_at, COALESCE(location,'') FROM interview_slots WHERE id=?`,
			slotID.String,
		).Scan(&start, &end, &loc)
	}

	var questions []map[string]any
	if questionsRaw.Valid && questionsRaw.String != "" {
		_ = json.Unmarshal([]byte(questionsRaw.String), &questions)
	}
	if questions == nil {
		questions = []map[string]any{}
	}

	ivName, _ := s.lookupInterviewerContact(ctx, claims.OpenID)
	hours := s.Cfg.InterviewerPackHours
	if hours <= 0 {
		hours = 24
	}

	return &PublicInterviewerPackView{
		ApplicationID: claims.AppID, CandidateName: candName, JDTitle: jdTitle,
		RoundIndex: claims.RoundIndex, RoundName: roundName, RoundTheme: roundTheme,
		InterviewStart: start, InterviewEnd: end, Location: loc,
		ExpiresAt: time.Unix(claims.Exp, 0), PackHours: hours,
		InterviewerName: ivName, Questions: questions,
	}, nil
}

func (s *Service) lookupInterviewerContact(ctx context.Context, openID string) (name, email string) {
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(name,''), COALESCE(email,'') FROM interviewer_profiles WHERE open_id=?`, openID,
	).Scan(&name, &email)
	if strings.TrimSpace(email) != "" {
		return name, email
	}
	if s.FeishuContact != nil && s.FeishuContact.Enabled() {
		if u, err := s.FeishuContact.GetUserByOpenID(ctx, openID); err == nil {
			if name == "" {
				name = u.Name
			}
			if u.Email != "" {
				email = u.Email
			}
		}
	}
	return name, email
}

func openIDInList(oid string, list []string) bool {
	for _, id := range list {
		if id == oid {
			return true
		}
	}
	return false
}
