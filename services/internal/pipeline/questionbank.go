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
	ID               string   `json:"id"`
	Category         string   `json:"category"`
	Title            string   `json:"title"`
	Content          string   `json:"content"`
	ReferenceAnswer  string   `json:"reference_answer"`
	ScoringPoints    []string `json:"scoring_points"`
	Tags             []string `json:"tags"`
	Difficulty       string   `json:"difficulty"`
	JDID             string   `json:"jd_id"`
	Enabled          *bool    `json:"enabled"`
}

func bankVectorPayload(item map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"id", "category", "title", "content", "tags", "difficulty", "jd_id", "enabled"} {
		if v, ok := item[k]; ok {
			out[k] = v
		}
	}
	if out["enabled"] == nil {
		out["enabled"] = true
	}
	return out
}

func (s *Service) ListQuestionBank(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, category, COALESCE(title,''), content, COALESCE(reference_answer,''),
		        COALESCE(scoring_points_json, JSON_ARRAY()), COALESCE(tags_json, JSON_ARRAY()),
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
		`SELECT id, category, COALESCE(title,''), content, COALESCE(reference_answer,''),
		        COALESCE(scoring_points_json, JSON_ARRAY()), COALESCE(tags_json, JSON_ARRAY()),
		        COALESCE(difficulty,'medium'), COALESCE(jd_id,''), enabled, created_at, updated_at
		 FROM question_bank WHERE id=?`, id)
	return scanBankRow(row)
}

type scannable interface {
	Scan(dest ...any) error
}

func scanBankRow(row scannable) (map[string]any, error) {
	var id, cat, title, content, refAns, diff, jdID string
	var scoringRaw, tagsRaw []byte
	var enabled bool
	var created, updated time.Time
	if err := row.Scan(&id, &cat, &title, &content, &refAns, &scoringRaw, &tagsRaw, &diff, &jdID, &enabled, &created, &updated); err != nil {
		return nil, err
	}
	var scoring []string
	if len(scoringRaw) > 0 {
		_ = json.Unmarshal(scoringRaw, &scoring)
	}
	var tags []string
	if len(tagsRaw) > 0 {
		_ = json.Unmarshal(tagsRaw, &tags)
	}
	if scoring == nil {
		scoring = []string{}
	}
	if tags == nil {
		tags = []string{}
	}
	return map[string]any{
		"id": id, "category": cat, "title": title, "content": content,
		"reference_answer": refAns, "scoring_points": scoring,
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
	scoring := in.ScoringPoints
	if scoring == nil {
		scoring = []string{}
	}
	scoringJSON, _ := json.Marshal(scoring)
	jdID := strings.TrimSpace(in.JDID)
	var jdAny any
	if jdID != "" {
		jdAny = jdID
	}

	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO question_bank (id, category, title, content, reference_answer, scoring_points_json, tags_json, difficulty, jd_id, enabled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE category=VALUES(category), title=VALUES(title), content=VALUES(content),
		   reference_answer=VALUES(reference_answer), scoring_points_json=VALUES(scoring_points_json),
		   tags_json=VALUES(tags_json), difficulty=VALUES(difficulty), jd_id=VALUES(jd_id), enabled=VALUES(enabled)`,
		id, cat, strings.TrimSpace(in.Title), content, strings.TrimSpace(in.ReferenceAnswer),
		scoringJSON, tagsJSON, diff, jdAny, enabled,
	)
	if err != nil {
		return nil, err
	}

	item, err := s.GetQuestionBank(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.Agent != nil {
		_, _ = s.Agent.RAGUpsert(ctx, bankVectorPayload(item))
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
		enabled = append(enabled, bankVectorPayload(it))
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

func (s *Service) buildQuestionBankHints(ctx context.Context, profile, jd map[string]any) ([]map[string]any, error) {
	if s.Agent == nil {
		return nil, nil
	}
	parts := []string{}
	if t, _ := jd["title"].(string); strings.TrimSpace(t) != "" {
		parts = append(parts, t)
	}
	if skills, ok := profile["skills"].([]any); ok {
		for _, sk := range skills {
			if st, ok := sk.(string); ok && st != "" {
				parts = append(parts, st)
			}
		}
	} else if skills, ok := profile["skills"].([]string); ok {
		parts = append(parts, skills...)
	}
	if req, ok := jd["requirements"].(map[string]any); ok {
		if rs, ok := req["skills"].([]any); ok {
			for _, sk := range rs {
				if st, ok := sk.(string); ok && st != "" {
					parts = append(parts, st)
				}
			}
		}
	}
	query := strings.TrimSpace(strings.Join(parts, " "))
	if query == "" {
		query = "interview questions"
	}
	docs, err := s.Agent.RAGQuery(ctx, query, 6)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	hints := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		bid, _ := d["id"].(string)
		bid = strings.TrimSpace(bid)
		if bid == "" {
			continue
		}
		if _, ok := seen[bid]; ok {
			continue
		}
		seen[bid] = struct{}{}
		row, err := s.GetQuestionBank(ctx, bid)
		if err != nil {
			text, _ := d["text"].(string)
			if text == "" {
				continue
			}
			hints = append(hints, map[string]any{
				"id": bid, "category": d["category"], "title": d["title"],
				"question": text, "reference_answer": "", "scoring_points": []string{},
				"difficulty": d["difficulty"],
			})
			continue
		}
		hints = append(hints, map[string]any{
			"id":               row["id"],
			"category":         row["category"],
			"title":            row["title"],
			"question":         row["content"],
			"reference_answer": row["reference_answer"],
			"scoring_points":   row["scoring_points"],
			"difficulty":       row["difficulty"],
		})
	}
	return hints, nil
}
