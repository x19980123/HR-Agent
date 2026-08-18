package pipeline

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// ParseQuestionBankCSV reads title,category,content[,difficulty][,tags][,enabled][,jd_id].
func ParseQuestionBankCSV(r io.Reader) ([]QuestionBankInput, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	var out []QuestionBankInput
	for i, rec := range records {
		if len(rec) == 0 {
			continue
		}
		for len(rec) < 7 {
			rec = append(rec, "")
		}
		title := strings.TrimSpace(rec[0])
		cat := strings.TrimSpace(rec[1])
		content := strings.TrimSpace(rec[2])
		if i == 0 && (strings.EqualFold(title, "title") || strings.EqualFold(title, "标题")) {
			continue
		}
		if content == "" {
			continue
		}
		diff := strings.TrimSpace(rec[3])
		if diff == "" {
			diff = "medium"
		}
		tags := []string{}
		if t := strings.TrimSpace(rec[4]); t != "" {
			for _, p := range strings.FieldsFunc(t, func(r rune) bool { return r == ',' || r == '，' || r == ';' }) {
				p = strings.TrimSpace(p)
				if p != "" {
					tags = append(tags, p)
				}
			}
		}
		enabled := true
		if e := strings.TrimSpace(strings.ToLower(rec[5])); e == "0" || e == "false" || e == "no" {
			enabled = false
		}
		out = append(out, QuestionBankInput{
			Title: title, Category: cat, Content: content, Difficulty: diff,
			Tags: tags, JDID: strings.TrimSpace(rec[6]), Enabled: &enabled,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid rows in csv")
	}
	return out, nil
}

func (s *Service) BatchUpsertQuestionBank(ctx context.Context, items []QuestionBankInput) (map[string]any, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("no items")
	}
	if len(items) > 500 {
		return nil, fmt.Errorf("too many items (max 500)")
	}
	var succeeded, failed int
	var errors []string
	for i, in := range items {
		if _, err := s.UpsertQuestionBank(ctx, in); err != nil {
			failed++
			errors = append(errors, fmt.Sprintf("row %d: %s", i+1, err.Error()))
		} else {
			succeeded++
		}
	}
	return map[string]any{
		"succeeded": succeeded, "failed": failed, "total": len(items), "errors": errors,
	}, nil
}
