package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hr-agent/services/internal/db"
	"github.com/hr-agent/services/internal/replytoken"
)

type PublicSlot struct {
	Index      int       `json:"index"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
	Location   string    `json:"location"`
	Status     string    `json:"status"`
	IsProposed bool      `json:"is_proposed"`
}

type PublicReplyView struct {
	ApplicationID    string       `json:"application_id"`
	CandidateName    string       `json:"candidate_name"`
	CandidateEmail   string       `json:"candidate_email"`
	JDTitle          string       `json:"jd_title"`
	Status           string       `json:"status"`
	ExpiresAt        time.Time    `json:"expires_at"`
	ReplyHours       int          `json:"reply_hours"`
	CanAct           bool         `json:"can_act"`
	CanUpdateEmail   bool         `json:"can_update_email"`
	Message          string       `json:"message,omitempty"`
	Slots            []PublicSlot `json:"slots"`
	RescheduleMode   bool         `json:"reschedule_mode"`
	RescheduleCount  int          `json:"reschedule_count"`
	MaxReschedule    int          `json:"max_reschedule"`
	CanRefreshSlots  bool         `json:"can_refresh_slots"`
}

// PublicReplyView verifies token and returns candidate-facing state.
func (s *Service) PublicReplyView(ctx context.Context, token string) (*PublicReplyView, error) {
	claims, err := replytoken.Verify(s.Cfg.ReplyTokenSecret, token)
	if err != nil {
		return nil, err
	}
	var name, email, status, jdID string
	var rescheduleCount int
	err = s.DB.QueryRowContext(ctx,
		`SELECT candidate_name, candidate_email, status, jd_id, reschedule_count FROM applications WHERE id=?`, claims.AppID,
	).Scan(&name, &email, &status, &jdID, &rescheduleCount)
	if err != nil {
		return nil, fmt.Errorf("application not found")
	}
	jdTitle := ""
	_ = s.DB.QueryRowContext(ctx, `SELECT title FROM job_descriptions WHERE id=?`, jdID).Scan(&jdTitle)

	slots, err := s.loadPublicSlots(ctx, claims.AppID)
	if err != nil {
		return nil, err
	}
	rescheduleMode := false
	for _, sl := range slots {
		if sl.IsProposed || sl.Status == "proposed" {
			rescheduleMode = true
			break
		}
	}
	hours := s.Cfg.ReplyTimeoutHours
	if hours <= 0 {
		hours = 48
	}
	maxR := s.Cfg.MaxReschedule
	if maxR <= 0 {
		maxR = 2
	}
	view := &PublicReplyView{
		ApplicationID:   claims.AppID,
		CandidateName:   name,
		CandidateEmail:  email,
		JDTitle:         jdTitle,
		Status:          status,
		ExpiresAt:       time.Unix(claims.Exp, 0),
		ReplyHours:      hours,
		CanAct:          status == db.StatusAwaitingReply,
		CanUpdateEmail:  status == db.StatusAwaitingReply,
		Slots:           slots,
		RescheduleMode:  rescheduleMode,
		RescheduleCount: rescheduleCount,
		MaxReschedule:   maxR,
		CanRefreshSlots: status == db.StatusAwaitingReply && rescheduleCount < maxR,
	}
	if !view.CanAct {
		switch status {
		case db.StatusConfirmed:
			view.Message = "时段已提交成功。请留意后续邮件通知（会议链接或线下地址将另行发送），无需再在本页操作。"
		case db.StatusNeedsHuman:
			view.Message = "已转交 HR 人工处理，我们会尽快与您联系，请留意邮件或电话。"
		case db.StatusDeclined:
			view.Message = "您已拒绝本次面试邀请，感谢您的回复。"
		default:
			view.Message = "该链接已处理或已失效，当前状态：" + status
		}
	}
	return view, nil
}

func (s *Service) loadPublicSlots(ctx context.Context, appID string) ([]PublicSlot, error) {
	// Include confirmed so admin detail still shows the final interview time after accept.
	rows, err := s.DB.QueryContext(ctx,
		`SELECT starts_at, ends_at, location, status, is_proposed
		 FROM interview_slots WHERE application_id=? AND status IN ('held','proposed','confirmed')
		 ORDER BY starts_at`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublicSlot
	i := 0
	for rows.Next() {
		var starts, ends time.Time
		var loc, st string
		var proposed int
		if err := rows.Scan(&starts, &ends, &loc, &st, &proposed); err != nil {
			return nil, err
		}
		out = append(out, PublicSlot{
			Index: i, StartsAt: starts, EndsAt: ends, Location: loc,
			Status: st, IsProposed: proposed == 1 || st == "proposed",
		})
		i++
	}
	return out, nil
}

// HandlePublicAction applies structured candidate actions without LLM classify.
func (s *Service) HandlePublicAction(ctx context.Context, token, action string, slotIndex *int, email string) error {
	claims, err := replytoken.Verify(s.Cfg.ReplyTokenSecret, token)
	if err != nil {
		return err
	}
	appID := claims.AppID

	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	var rescheduleCount int
	if err := tx.QueryRowContext(ctx,
		`SELECT status, reschedule_count FROM applications WHERE id=? FOR UPDATE`, appID,
	).Scan(&status, &rescheduleCount); err != nil {
		return err
	}
	if status != db.StatusAwaitingReply {
		return fmt.Errorf("cannot act on status=%s", status)
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	switch action {
	case "update_contact":
		if email == "" {
			return fmt.Errorf("email required")
		}
		return s.UpdateApplicationContactFromCandidate(ctx, appID, email)
	case "accept", "pick_slot":
		return s.confirm(ctx, appID, slotIndex)
	case "decline":
		return s.decline(ctx, appID)
	case "reschedule":
		if rescheduleCount >= s.Cfg.MaxReschedule {
			return s.markHuman(ctx, appID, "reschedule_limit_exceeded")
		}
		// In-page slot refresh only — no second email.
		return s.refreshProposedSlots(ctx, appID, nil)
	case "none_suitable", "slots_unsuitable":
		return s.markHuman(ctx, appID, "candidate_rejected_all_slots")
	default:
		return fmt.Errorf("unknown action %q", action)
	}
}
