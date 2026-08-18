package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

func (s *Service) preExtractBundle(ctx context.Context, resumePath string) (email, name string, confidence float64, metaJSON string, err error) {
	if s.Cfg == nil || !s.Cfg.ImportPreExtract || s.Agent == nil {
		return "", "", 0, "", nil
	}
	resumePath = s.resolveResumePath(resumePath)
	out, err := s.Agent.ExtractContact(ctx, resumePath, "")
	if err != nil {
		return "", "", 0, "", err
	}
	meta, _ := json.Marshal(map[string]any{
		"email": out.Email, "name": out.Name, "confidence": out.Confidence, "candidates": out.Candidates,
	})
	return strings.TrimSpace(out.Email), strings.TrimSpace(out.Name), out.Confidence, string(meta), nil
}

func (s *Service) PreExtractContact(ctx context.Context, resumePath string) (email, name string, confidence float64, err error) {
	e, n, c, _, err := s.preExtractBundle(ctx, resumePath)
	return e, n, c, err
}

func (s *Service) ResolveImportContact(
	ctx context.Context,
	resumePath, uploadName, defaultEmail string,
	csvRow CSVMappingRow,
	hasCSV bool,
	index, total int,
) (name, email, source, preExtractEmail, preExtractMeta string) {
	name = strings.TrimSuffix(filepath.Base(uploadName), filepath.Ext(uploadName))
	pe, pn, _, meta, _ := s.preExtractBundle(ctx, resumePath)
	preExtractEmail, preExtractMeta = pe, meta

	if hasCSV && csvRow.Email != "" && s.validateEmailFormat(csvRow.Email) {
		email = csvRow.Email
		if csvRow.Name != "" {
			name = csvRow.Name
		}
		return name, email, ContactSourceImportCSV, preExtractEmail, preExtractMeta
	}
	if pe != "" && s.validateEmailFormat(pe) {
		email = pe
		if pn != "" {
			name = pn
		}
		return name, email, ContactSourcePreExtract, preExtractEmail, preExtractMeta
	}
	email = ImportItemEmail(defaultEmail, index, total)
	return name, email, ContactSourceImportPlaceholder, preExtractEmail, preExtractMeta
}

// ImportItemEmail assigns one email per resume file in a batch.
func ImportItemEmail(defaultEmail string, index, total int) string {
	defaultEmail = strings.TrimSpace(defaultEmail)
	if defaultEmail == "" {
		defaultEmail = "candidate@import.local"
	}
	if total <= 1 {
		return defaultEmail
	}
	domain := defaultEmail
	if at := strings.LastIndex(defaultEmail, "@"); at >= 0 {
		domain = defaultEmail[at+1:]
	}
	if domain == "" {
		domain = "import.local"
	}
	return fmt.Sprintf("import%d@%s", index+1, domain)
}
