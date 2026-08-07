package pii

import (
	"regexp"
	"strings"
)

var (
	rePhone = regexp.MustCompile(`1[3-9]\d{9}`)
	reEmail = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	reID    = regexp.MustCompile(`\b\d{17}[\dXx]\b`)
)

// Redact masks common PII in free text for logs/audit.
func Redact(s string) string {
	s = rePhone.ReplaceAllStringFunc(s, func(m string) string {
		if len(m) < 7 {
			return "***"
		}
		return m[:3] + "****" + m[len(m)-4:]
	})
	s = reEmail.ReplaceAllStringFunc(s, func(m string) string {
		parts := strings.Split(m, "@")
		if len(parts) != 2 {
			return "***@***"
		}
		name := parts[0]
		if len(name) <= 1 {
			return "*@" + parts[1]
		}
		return string(name[0]) + "***@" + parts[1]
	})
	s = reID.ReplaceAllString(s, "******************")
	return s
}
