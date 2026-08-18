package pipeline

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ImportJobItem struct {
	CandidateName     string
	CandidateEmail    string
	CandidateSource   string
	ExternalID        string
	ResumePath        string
	PreExtractEmail   string
	PreExtractMetaJSON string
}

func nullJSONText(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if !json.Valid([]byte(s)) {
		b, _ := json.Marshal(s)
		return string(b)
	}
	return s
}

func (s *Service) CreateImportJob(ctx context.Context, jdID, createdBy string, items []ImportJobItem) (string, error) {
	jdID = strings.TrimSpace(jdID)
	if jdID == "" {
		return "", fmt.Errorf("jd_id required")
	}
	if len(items) == 0 {
		return "", fmt.Errorf("no resumes")
	}
	jobID := uuid.NewString()
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO import_jobs (id, created_by, jd_id, status, total) VALUES (?, ?, ?, 'pending', ?)`,
		jobID, createdBy, jdID, len(items),
	)
	if err != nil {
		return "", err
	}
	for _, it := range items {
		itemID := uuid.NewString()
		sum := sha256File(it.ResumePath)
		_, err = s.DB.ExecContext(ctx,
			`INSERT INTO import_items (id, job_id, candidate_name, candidate_email, email_source, pre_extract_email, pre_extract_meta, external_id, resume_path, resume_sha256, status)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending')`,
			itemID, jobID, it.CandidateName, it.CandidateEmail, nullStr(it.CandidateSource),
			nullStr(it.PreExtractEmail), nullJSONText(it.PreExtractMetaJSON), nullStr(it.ExternalID), it.ResumePath, sum,
		)
		if err != nil {
			return "", err
		}
	}
	go s.runImportJob(jobID, jdID)
	return jobID, nil
}

func (s *Service) runImportJob(jobID, jdID string) {
	ctx := context.Background()
	_, _ = s.DB.ExecContext(ctx, `UPDATE import_jobs SET status='running' WHERE id=?`, jobID)
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, candidate_name, candidate_email, COALESCE(email_source,''), COALESCE(external_id,''), resume_path FROM import_items WHERE job_id=? AND status='pending'`, jobID)
	if err != nil {
		_, _ = s.DB.ExecContext(ctx, `UPDATE import_jobs SET status='failed', finished_at=NOW() WHERE id=?`, jobID)
		return
	}
	defer rows.Close()
	ok, fail := 0, 0
	sem := make(chan struct{}, s.importConcurrency())
	for rows.Next() {
		var itemID, name, email, path, src, extID string
		if err := rows.Scan(&itemID, &name, &email, &src, &extID, &path); err != nil {
			continue
		}
		sem <- struct{}{}
		n, e, p, source, externalID := name, email, path, src, extID
		id := itemID
		sum := sha256File(p)
		if skip, dupID, derr := s.importDedupSkip(ctx, jdID, e, sum, externalID); derr != nil {
			fail++
			_, _ = s.DB.ExecContext(ctx, `UPDATE import_items SET status='error', error_message=? WHERE id=?`, derr.Error(), id)
			<-sem
			continue
		} else if skip {
			appRef := dupID
			if appRef == "" {
				appRef = ""
			}
			_, _ = s.DB.ExecContext(ctx, `UPDATE import_items SET status='ok', application_id=NULLIF(?, ''), error_message='dedup_skip' WHERE id=?`, appRef, id)
			ok++
			<-sem
			continue
		}
		_, _ = s.DB.ExecContext(ctx, `UPDATE import_items SET status='running' WHERE id=?`, id)
		appID, runErr := s.Start(ctx, StartInput{
			JDID: jdID, CandidateName: n, CandidateEmail: e, ResumePath: p, ContactSource: source, ExternalID: externalID,
		})
		if runErr != nil {
			fail++
			_, _ = s.DB.ExecContext(ctx,
				`UPDATE import_items SET status='error', error_message=? WHERE id=?`, runErr.Error(), id)
		} else {
			ok++
			_, _ = s.DB.ExecContext(ctx,
				`UPDATE import_items SET status='ok', application_id=? WHERE id=?`, appID, id)
		}
		<-sem
	}
	st := "completed"
	if fail > 0 && ok == 0 {
		st = "failed"
	}
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE import_jobs SET status=?, succeeded=?, failed=?, finished_at=NOW() WHERE id=?`,
		st, ok, fail, jobID)
}

func (s *Service) importConcurrency() int {
	if s.Cfg != nil && s.Cfg.ImportConcurrency > 0 {
		return s.Cfg.ImportConcurrency
	}
	return 2
}

func (s *Service) GetImportJob(ctx context.Context, jobID string) (map[string]any, error) {
	var jdID, status, createdBy string
	var total, succeeded, failed int
	var created time.Time
	var finished sql.NullTime
	err := s.DB.QueryRowContext(ctx,
		`SELECT jd_id, status, created_by, total, succeeded, failed, created_at, finished_at FROM import_jobs WHERE id=?`, jobID,
	).Scan(&jdID, &status, &createdBy, &total, &succeeded, &failed, &created, &finished)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"id": jobID, "jd_id": jdID, "status": status, "created_by": createdBy,
		"total": total, "succeeded": succeeded, "failed": failed, "created_at": created,
	}
	if finished.Valid {
		out["finished_at"] = finished.Time
	}
	return out, nil
}

func sha256File(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	_, _ = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

// SaveImportResume stores uploaded file under upload dir.
func (s *Service) SaveImportResume(name string, r io.Reader) (string, error) {
	ext := filepath.Ext(name)
	if ext == "" {
		ext = ".bin"
	}
	dest := filepath.Join(s.Cfg.UploadDir, uuid.NewString()+ext)
	if err := os.MkdirAll(s.Cfg.UploadDir, 0o755); err != nil {
		return "", err
	}
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, r); err != nil {
		return "", err
	}
	if abs, err := filepath.Abs(dest); err == nil {
		return abs, nil
	}
	return dest, nil
}
