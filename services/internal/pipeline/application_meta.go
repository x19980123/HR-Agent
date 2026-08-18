package pipeline

import (
	"context"

	"github.com/hr-agent/services/internal/alert"
	"github.com/hr-agent/services/internal/audit"
	"github.com/hr-agent/services/internal/db"
)

const (
	ErrorKindNone     = "none"
	ErrorKindBusiness = "business"
	ErrorKindSystem   = "system"
)

func (s *Service) setApplicationErrorMeta(ctx context.Context, appID, errorKind, humanCode, systemCode, screenTier string) {
	if errorKind == "" {
		errorKind = ErrorKindNone
	}
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE applications SET error_kind=?, human_reason_code=?, system_error_code=?, screen_tier=?
		 WHERE id=?`,
		errorKind,
		nullStr(humanCode),
		nullStr(systemCode),
		nullStr(screenTier),
		appID,
	)
}

func (s *Service) markSystemFailed(ctx context.Context, appID, systemCode, message string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE applications SET status=?, error_message=?, error_kind=?, system_error_code=?, human_reason_code=NULL WHERE id=?`,
		db.StatusFailed, message, ErrorKindSystem, systemCode, appID,
	)
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "pipeline_system_failed",
		AfterStatus:   db.StatusFailed,
		Detail: map[string]any{
			"system_error_code": systemCode,
			"error":             message,
		},
	})
	if s.Cfg != nil && s.Cfg.AlertWebhookURL != "" {
		url := s.Cfg.AlertWebhookURL
		go func() {
			_ = alert.Notify(context.Background(), url,
				"[HR-Agent] 系统故障",
				"app="+appID+" code="+systemCode+" "+message)
		}()
	}
	return err
}

func (s *Service) markHumanWithCode(ctx context.Context, appID, reason, humanCode string) error {
	if humanCode == "" {
		humanCode = "needs_human"
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE applications SET status=?, error_message=?, error_kind=?, human_reason_code=?, system_error_code=NULL WHERE id=?`,
		db.StatusNeedsHuman, reason, ErrorKindBusiness, humanCode, appID,
	)
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "needs_human",
		AfterStatus:   db.StatusNeedsHuman,
		Detail:        map[string]any{"reason": reason, "human_reason_code": humanCode},
	})
	return err
}
