package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (s *Service) importDedupSkip(ctx context.Context, jdID, email, sha256, externalID string) (bool, string, error) {
	action := "skip"
	if s.Cfg != nil && s.Cfg.ImportDedup != "" {
		action = strings.ToLower(s.Cfg.ImportDedup)
	}
	if action == "new_version" || action == "" {
		return false, "", nil
	}
	externalID = strings.TrimSpace(externalID)
	if externalID != "" {
		var id string
		err := s.DB.QueryRowContext(ctx,
			`SELECT id FROM applications WHERE jd_id=? AND external_id=? ORDER BY created_at DESC LIMIT 1`,
			jdID, externalID,
		).Scan(&id)
		if err == nil && id != "" {
			if action == "error" {
				return true, "", fmt.Errorf("duplicate external_id for jd")
			}
			return true, id, nil
		}
		if err != nil && err != sql.ErrNoRows {
			return false, "", err
		}
		err = s.DB.QueryRowContext(ctx,
			`SELECT application_id FROM import_items WHERE job_id IN (SELECT id FROM import_jobs WHERE jd_id=?)
			 AND external_id=? AND status='ok' AND application_id IS NOT NULL LIMIT 1`, jdID, externalID,
		).Scan(&id)
		if err == nil && id != "" {
			if action == "error" {
				return true, "", fmt.Errorf("duplicate external_id for jd")
			}
			return true, id, nil
		}
	}
	email = strings.TrimSpace(strings.ToLower(email))
	if email != "" && !s.isPlaceholderEmail(email) {
		var id string
		err := s.DB.QueryRowContext(ctx,
			`SELECT id FROM applications WHERE jd_id=? AND LOWER(candidate_email)=? ORDER BY created_at DESC LIMIT 1`,
			jdID, email,
		).Scan(&id)
		if err == nil && id != "" {
			if action == "error" {
				return true, "", fmt.Errorf("duplicate application for jd+email")
			}
			return true, id, nil
		}
		if err != nil && err != sql.ErrNoRows {
			return false, "", err
		}
	}
	if sha256 != "" {
		var id string
		err := s.DB.QueryRowContext(ctx,
			`SELECT a.id FROM applications a
			 INNER JOIN import_items ii ON ii.application_id=a.id
			 WHERE a.jd_id=? AND ii.resume_sha256=? LIMIT 1`, jdID, sha256,
		).Scan(&id)
		if err == nil && id != "" {
			if action == "error" {
				return true, "", fmt.Errorf("duplicate resume sha256 for jd")
			}
			return true, id, nil
		}
	}
	return false, "", nil
}
