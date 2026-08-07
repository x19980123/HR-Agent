package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type QuestionBankInput struct {
	ID         string   `json:"id"`
	Category   string   `json:"category"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags"`
	Difficulty string   `json:"difficulty"`
	JDID       string   `json:"jd_id"`
	Enabled    *bool    `json:"enabled"`
}

func (s *Service) ListQuestionBank(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, category, COALESCE(title,''), content, COALESCE(tags_json, JSON_ARRAY()),
		        COALESCE(difficulty,'medium'), COALESCE(jd_id,''), enabled, created_at, updated_at
		 FROM question_bank ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		item, err := scanBankRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) GetQuestionBank(ctx context.Context, id string) (map[string]any, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, category, COALESCE(title,''), content, COALESCE(tags_json, JSON_ARRAY()),
		        COALESCE(difficulty,'medium'), COALESCE(jd_id,''), enabled, created_at, updated_at
		 FROM question_bank WHERE id=?`, id)
	return scanBankRow(row)
}

type scannable interface {
	Scan(dest ...any) error
}

func scanBankRow(row scannable) (map[string]any, error) {
	var id, cat, title, content, diff, jdID string
	var tagsRaw []byte
	var enabled bool
	var created, updated time.Time
	if err := row.Scan(&id, &cat, &title, &content, &tagsRaw, &diff, &jdID, &enabled, &created, &updated); err != nil {
		return nil, err
	}
	var tags []string
	if len(tagsRaw) > 0 {
		_ = json.Unmarshal(tagsRaw, &tags)
	}
	if tags == nil {
		tags = []string{}
	}
	return map[string]any{
		"id": id, "category": cat, "title": title, "content": content,
		"tags": tags, "difficulty": diff, "jd_id": jdID, "enabled": enabled,
		"created_at": created, "updated_at": updated,
	}, nil
}

func (s *Service) UpsertQuestionBank(ctx context.Context, in QuestionBankInput) (map[string]any, error) {
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, fmt.Errorf("content required")
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = "qb-" + uuid.NewString()[:10]
	}
	cat := strings.TrimSpace(in.Category)
	if cat == "" {
		cat = "fundamentals"
	}
	diff := strings.TrimSpace(in.Difficulty)
	if diff == "" {
		diff = "medium"
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	tags := in.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, _ := json.Marshal(tags)
	jdID := strings.TrimSpace(in.JDID)
	var jdAny any
	if jdID != "" {
		jdAny = jdID
	}

	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO question_bank (id, category, title, content, tags_json, difficulty, jd_id, enabled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE category=VALUES(category), title=VALUES(title), content=VALUES(content),
		   tags_json=VALUES(tags_json), difficulty=VALUES(difficulty), jd_id=VALUES(jd_id), enabled=VALUES(enabled)`,
		id, cat, strings.TrimSpace(in.Title), content, tagsJSON, diff, jdAny, enabled,
	)
	if err != nil {
		return nil, err
	}

	item, err := s.GetQuestionBank(ctx, id)
	if err != nil {
		return nil, err
	}
	// Best-effort vector sync; MySQL remains source of truth.
	if s.Agent != nil {
		_, _ = s.Agent.RAGUpsert(ctx, item)
	}
	return item, nil
}

func (s *Service) DeleteQuestionBank(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM question_bank WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if s.Agent != nil {
		_ = s.Agent.RAGDelete(ctx, id)
	}
	return nil
}

func (s *Service) ReindexQuestionBank(ctx context.Context) (map[string]any, error) {
	items, err := s.ListQuestionBank(ctx)
	if err != nil {
		return nil, err
	}
	enabled := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if en, ok := it["enabled"].(bool); ok && !en {
			continue
		}
		enabled = append(enabled, it)
	}
	if s.Agent == nil {
		return map[string]any{"ok": false, "error": "agent unavailable", "items": len(enabled)}, nil
	}
	res, err := s.Agent.RAGReindex(ctx, enabled)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error(), "items": len(enabled)}, nil
	}
	res["mysql_items"] = len(items)
	res["synced_items"] = len(enabled)
	return res, nil
}
