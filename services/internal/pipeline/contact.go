package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/hr-agent/services/internal/audit"
	"github.com/hr-agent/services/internal/db"
)

const (
	ContactSourceHRForm            = "hr_form"
	ContactSourceImportCSV         = "import_csv"
	ContactSourceImportPlaceholder = "import_placeholder"
	ContactSourcePreExtract        = "pre_extract"
	ContactSourceParseProfile      = "parse_profile"
	ContactSourceHumanOverride     = "human_override"
	ContactSourceCandidateSelf     = "candidate_self"
)

var emailFormatRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

func (s *Service) placeholderDomains() []string {
	if s.Cfg != nil && len(s.Cfg.ContactPlaceholderDomains) > 0 {
		return s.Cfg.ContactPlaceholderDomains
	}
	return []string{"import.local"}
}

func (s *Service) blocklistDomains() []string {
	if s.Cfg != nil && len(s.Cfg.ContactBlocklistDomains) > 0 {
		return s.Cfg.ContactBlocklistDomains
	}
	return []string{"example.com", "test.com", "localhost"}
}

func (s *Service) contactRequireResolved() bool {
	if s.Cfg == nil {
		return true
	}
	return s.Cfg.ContactRequireResolved
}

func (s *Service) validateEmailFormat(email string) bool {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 254 {
		return false
	}
	if !emailFormatRegex.MatchString(email) {
		return false
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, b := range s.blocklistDomains() {
		b = strings.ToLower(b)
		if domain == b || strings.HasSuffix(domain, "."+b) {
			return false
		}
	}
	return true
}

func (s *Service) isPlaceholderEmail(email string) bool {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return true
	}
	local, domain := email[:at], email[at+1:]
	for _, d := range s.placeholderDomains() {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if strings.Contains(d, "@") {
			if email == strings.ToLower(d) {
				return true
			}
			continue
		}
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return true
		}
	}
	if strings.HasPrefix(local, "import") {
		return true
	}
	return false
}

func contactSourceRank(source string) int {
	switch source {
	case ContactSourceHumanOverride:
		return 6
	case ContactSourceCandidateSelf:
		return 5
	case ContactSourceHRForm:
		return 5
	case ContactSourceImportCSV:
		return 4
	case ContactSourceParseProfile:
		return 3
	case ContactSourcePreExtract:
		return 2
	case ContactSourceImportPlaceholder:
		return 1
	default:
		return 0
	}
}

func inferStartContactSource(email, explicit string) string {
	if explicit != "" {
		return explicit
	}
	e := strings.TrimSpace(email)
	svc := &Service{}
	if e != "" && svc.validateEmailFormat(e) && !svc.isPlaceholderEmail(e) {
		return ContactSourceHRForm
	}
	return ContactSourceImportPlaceholder
}

// FormContactSourceForEmail tags single-upload form emails for provenance.
func (s *Service) FormContactSourceForEmail(email string) string {
	email = strings.TrimSpace(email)
	if email != "" && s.validateEmailFormat(email) && !s.isPlaceholderEmail(email) {
		return ContactSourceHRForm
	}
	if email != "" {
		return ContactSourceImportPlaceholder
	}
	return ContactSourceImportPlaceholder
}

func profileStringField(profile map[string]any, key string) string {
	if profile == nil {
		return ""
	}
	v, ok := profile[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func profileFloat(profile map[string]any, key string) float64 {
	if profile == nil {
		return 0
	}
	v, ok := profile[key]
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	default:
		return 0
	}
}

func (s *Service) applyProfileContact(ctx context.Context, appID string, profile map[string]any) {
	pe := profileStringField(profile, "email")
	pn := profileStringField(profile, "name")
	if pe == "" && pn == "" {
		return
	}
	var curEmail, curName, curSource sql.NullString
	_ = s.DB.QueryRowContext(ctx,
		`SELECT candidate_email, candidate_name, contact_email_source FROM applications WHERE id=?`, appID,
	).Scan(&curEmail, &curName, &curSource)

	email := curEmail.String
	name := curName.String
	source := curSource.String
	if source == "" {
		if s.isPlaceholderEmail(email) {
			source = ContactSourceImportPlaceholder
		} else if s.validateEmailFormat(email) {
			source = ContactSourceHRForm
		}
	}

	newEmail, newSource := email, source
	if pe != "" && s.validateEmailFormat(pe) {
		if contactSourceRank(ContactSourceParseProfile) > contactSourceRank(source) || s.isPlaceholderEmail(email) {
			newEmail = pe
			newSource = ContactSourceParseProfile
		}
	}
	newName := name
	if pn != "" && len([]rune(pn)) >= 1 && len([]rune(pn)) <= 40 && !strings.Contains(pn, "@") {
		if name == "" || s.isPlaceholderEmail(email) {
			newName = pn
		}
	}
	effectiveEmail := email
	if newEmail != email || newName != name {
		conf := 0.85
		if c := profileFloat(profile, "parse_confidence"); c > 0 {
			conf = c
		}
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE applications SET candidate_email=?, candidate_name=?,
			 contact_email_source=?, contact_email_confidence=?, contact_resolved_at=NOW() WHERE id=?`,
			newEmail, newName, newSource, conf, appID,
		)
		_ = s.Audit.Log(ctx, audit.Event{
			ApplicationID: appID,
			Action:        "contact_resolved_from_parse",
			Detail: map[string]any{
				"before_email": email, "after_email": newEmail,
				"before_source": source, "after_source": newSource,
				"profile_email": pe,
			},
		})
		effectiveEmail = newEmail
	}
	if pe != "" {
		s.auditImportContactMismatch(ctx, appID, effectiveEmail, pe)
	}
}

func normEmail(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}

func (s *Service) auditImportContactMismatch(ctx context.Context, appID, effectiveEmail, profileEmail string) {
	var itemEmail, emailSource, preExtract sql.NullString
	var preMeta sql.NullString
	err := s.DB.QueryRowContext(ctx,
		`SELECT candidate_email, COALESCE(email_source,''), pre_extract_email, pre_extract_meta
		 FROM import_items WHERE application_id=? ORDER BY id DESC LIMIT 1`, appID,
	).Scan(&itemEmail, &emailSource, &preExtract, &preMeta)
	if err != nil {
		return
	}
 prof := normEmail(profileEmail)
 src := emailSource.String

	detail := map[string]any{"effective_email": effectiveEmail, "profile_email": profileEmail, "import_source": src}
	mismatch := false

	if src == ContactSourceImportCSV && itemEmail.Valid && s.validateEmailFormat(itemEmail.String) {
		csvEm := normEmail(itemEmail.String)
		if prof != "" && prof != csvEm {
			detail["csv_email"] = itemEmail.String
			detail["kind"] = "csv_vs_parse"
			mismatch = true
		}
	}
	if preExtract.Valid && preExtract.String != "" && s.validateEmailFormat(preExtract.String) {
		pre := normEmail(preExtract.String)
		if prof != "" && prof != pre && src != ContactSourceImportCSV {
			detail["pre_extract_email"] = preExtract.String
			detail["kind"] = "pre_extract_vs_parse"
			mismatch = true
		}
		if prof != "" && preMeta.Valid && preMeta.String != "" {
			if !emailInPreExtractCandidates(prof, preMeta.String) && prof != pre {
				detail["candidates_miss"] = true
				if detail["kind"] == nil {
					detail["kind"] = "pre_extract_candidates_miss"
				}
				mismatch = true
			}
		}
	}
	if !mismatch {
		return
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "contact_email_mismatch",
		Detail:        detail,
	})
	if src == ContactSourceImportCSV {
		reason := "CSV 映射邮箱与解析结果不一致，请 HR 确认联系方式"
		_ = s.markHumanWithCode(ctx, appID, reason, "contact_csv_parse_mismatch")
	}
}

func emailInPreExtractCandidates(profileEmail, metaJSON string) bool {
	var meta struct {
		Candidates []string `json:"candidates"`
	}
	if json.Unmarshal([]byte(metaJSON), &meta) != nil {
		return false
	}
	pe := normEmail(profileEmail)
	for _, c := range meta.Candidates {
		if normEmail(c) == pe {
			return true
		}
	}
	return false
}

func (s *Service) contactGate(ctx context.Context, appID string) error {
	var email string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT candidate_email FROM applications WHERE id=?`, appID,
	).Scan(&email); err != nil {
		return err
	}
	if !s.contactRequireResolved() {
		return nil
	}
	if s.Cfg != nil && s.Cfg.MailDryRun && s.Cfg.ContactAllowPlaceholderInDryRun && s.isPlaceholderEmail(email) {
		return nil
	}
	if s.isPlaceholderEmail(email) || !s.validateEmailFormat(email) {
		reason := "简历未解析出有效邮箱，请人工填写后重试或人工通过"
		return s.markHumanWithCode(ctx, appID, reason, "contact_missing")
	}
	return nil
}

func (s *Service) setContactOnCreate(ctx context.Context, appID, email, source string, confidence float64) {
	if source == "" {
		source = inferStartContactSource(email, "")
	}
	var conf any = nil
	if confidence > 0 {
		conf = confidence
	}
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE applications SET contact_email_source=?, contact_email_confidence=?, contact_resolved_at=IF(? IS NOT NULL, NOW(), NULL) WHERE id=?`,
		source, conf, conf, appID,
	)
}

func (s *Service) UpdateApplicationContact(ctx context.Context, appID, email, name string) error {
	email = strings.TrimSpace(email)
	name = strings.TrimSpace(name)
	if email != "" && !s.validateEmailFormat(email) {
		return fmt.Errorf("invalid email")
	}
	if email == "" {
		return fmt.Errorf("email required")
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE applications SET candidate_email=?, candidate_name=IF(?='', candidate_name, ?),
		 contact_email_source=?, contact_email_confidence=1.0, contact_resolved_at=NOW() WHERE id=?`,
		email, name, name, ContactSourceHumanOverride, appID,
	)
	if err != nil {
		return err
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "contact_human_override",
		Detail:        map[string]any{"email": email, "name": name},
	})
	return nil
}

func (s *Service) UpdateApplicationContactFromCandidate(ctx context.Context, appID, email string) error {
	email = strings.TrimSpace(email)
	if !s.validateEmailFormat(email) {
		return fmt.Errorf("invalid email")
	}
	if s.isPlaceholderEmail(email) {
		return fmt.Errorf("placeholder email not allowed")
	}
	var status string
	if err := s.DB.QueryRowContext(ctx, `SELECT status FROM applications WHERE id=?`, appID).Scan(&status); err != nil {
		return err
	}
	if status != db.StatusAwaitingReply {
		return fmt.Errorf("contact update only allowed while awaiting reply")
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE applications SET candidate_email=?, contact_email_source=?, contact_email_confidence=1.0, contact_resolved_at=NOW() WHERE id=?`,
		email, ContactSourceCandidateSelf, appID,
	)
	if err != nil {
		return err
	}
	_ = s.Audit.Log(ctx, audit.Event{
		ApplicationID: appID,
		Action:        "contact_candidate_self",
		Detail:        map[string]any{"email": email},
	})
	return nil
}
