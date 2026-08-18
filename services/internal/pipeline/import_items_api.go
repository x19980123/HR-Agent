package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type ImportItemRow struct {
	ID              string
	Status          string
	CandidateName   string
	CandidateEmail  string
	EmailSource     string
	ExternalID      string
	ApplicationID   string
	ErrorMessage    string
	ResumePath      string
}

func (s *Service) ListImportItems(ctx context.Context, jobID, status string, limit, offset int) ([]map[string]any, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	q := `SELECT id, status, candidate_name, candidate_email, COALESCE(email_source,''),
	       COALESCE(external_id,''), COALESCE(application_id,''), COALESCE(error_message,'')
	       FROM import_items WHERE job_id=?`
	args := []any{jobID}
	if st := strings.TrimSpace(status); st != "" {
		q += ` AND status=?`
		args = append(args, st)
	}
	q += ` ORDER BY created_at LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, st, name, email, src, ext, appID, errMsg string
		if err := rows.Scan(&id, &st, &name, &email, &src, &ext, &appID, &errMsg); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "status": st, "candidate_name": name, "candidate_email": email,
			"email_source": src, "external_id": ext, "application_id": appID, "error_message": errMsg,
		})
	}
	return out, nil
}

func (s *Service) RetryImportItem(ctx context.Context, jobID, itemID, jdID string) error {
	jobID = strings.TrimSpace(jobID)
	itemID = strings.TrimSpace(itemID)
	var row ImportItemRow
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, status, candidate_name, candidate_email, COALESCE(email_source,''),
		        COALESCE(external_id,''), COALESCE(application_id,''), COALESCE(error_message,''), resume_path
		 FROM import_items WHERE id=? AND job_id=?`, itemID, jobID,
	).Scan(&row.ID, &row.Status, &row.CandidateName, &row.CandidateEmail, &row.EmailSource,
		&row.ExternalID, &row.ApplicationID, &row.ErrorMessage, &row.ResumePath)
	if err == sql.ErrNoRows {
		return fmt.Errorf("import item not found")
	}
	if err != nil {
		return err
	}
	if row.Status != "error" {
		return fmt.Errorf("only failed items can be retried (status=%s)", row.Status)
	}
	if jdID == "" {
		_ = s.DB.QueryRowContext(ctx, `SELECT jd_id FROM import_jobs WHERE id=?`, jobID).Scan(&jdID)
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE import_items SET status='pending', error_message=NULL, application_id=NULL WHERE id=?`, itemID)
	if err != nil {
		return err
	}
	go s.processImportItem(jobID, jdID, row)
	return nil
}

func (s *Service) processImportItem(jobID, jdID string, row ImportItemRow) {
	ctx := context.Background()
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	defer func() { <-sem }()

	sum := sha256File(row.ResumePath)
	if skip, dupID, derr := s.importDedupSkip(ctx, jdID, row.CandidateEmail, sum, row.ExternalID); derr != nil {
		_, _ = s.DB.ExecContext(ctx, `UPDATE import_items SET status='error', error_message=? WHERE id=?`, derr.Error(), row.ID)
		s.bumpImportJobCounts(ctx, jobID, false)
		return
	} else if skip {
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE import_items SET status='ok', application_id=NULLIF(?, ''), error_message='dedup_skip' WHERE id=?`, dupID, row.ID)
		s.bumpImportJobCounts(ctx, jobID, true)
		return
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE import_items SET status='running' WHERE id=?`, row.ID)
	appID, runErr := s.Start(ctx, StartInput{
		JDID: jdID, CandidateName: row.CandidateName, CandidateEmail: row.CandidateEmail,
		ResumePath: row.ResumePath, ContactSource: row.EmailSource, ExternalID: row.ExternalID,
	})
	if runErr != nil {
		_, _ = s.DB.ExecContext(ctx, `UPDATE import_items SET status='error', error_message=? WHERE id=?`, runErr.Error(), row.ID)
		s.bumpImportJobCounts(ctx, jobID, false)
		return
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE import_items SET status='ok', application_id=? WHERE id=?`, appID, row.ID)
	s.bumpImportJobCounts(ctx, jobID, true)
}

func (s *Service) bumpImportJobCounts(ctx context.Context, jobID string, ok bool) {
	if ok {
		_, _ = s.DB.ExecContext(ctx, `UPDATE import_jobs SET succeeded=succeeded+1, failed=GREATEST(failed-1,0) WHERE id=?`, jobID)
	} else {
		_, _ = s.DB.ExecContext(ctx, `UPDATE import_jobs SET failed=failed+1 WHERE id=?`, jobID)
	}
}
