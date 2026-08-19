package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPAddr          string
	MySQLDSN          string
	UploadDir         string
	AgentBaseURL      string
	SMTPHost          string
	SMTPPort          int
	SMTPUser          string
	SMTPPass          string
	SMTPFrom          string
	MailDryRun        bool
	IMAPHost          string
	IMAPPort          int
	IMAPUser          string
	IMAPPass          string
	ReplyTimeoutHours int
	InterviewerPackHours int
	MaxReschedule     int

	PublicBaseURL     string
	ReplyTokenSecret  string
	HRAPIToken        string
	InternalAPIToken  string
	SessionSecret     string

	CalendarProvider        string
	FeishuAppID             string
	FeishuAppSecret         string
	FeishuCalendarID        string
	FeishuUserIDType        string
	FeishuTimezone          string
	FeishuLocation          string

	// Feishu admin login (OAuth)
	FeishuLoginEnabled   bool
	FeishuOAuthRedirect  string
	FeishuHRAllowOpenIDs []string
	FeishuHRAllowEmails  []string

	AlertWebhookURL    string
	ImportConcurrency  int

	ContactRequireResolved           bool
	ContactAllowPlaceholderInDryRun  bool
	ContactPlaceholderDomains        []string
	ContactBlocklistDomains          []string
	ImportPreExtract                 bool
	ImportDedup                      string // skip | error | new_version

	// Scheduling Agent (Python assign + verify); false = Phase 2 classified resolve only.
	SchedulingAgentEnabled bool
}

func Load() (*Config, error) {
	loadDotEnv()

	uploadDir := getenv("UPLOAD_DIR", "./uploads")
	if abs, err := filepath.Abs(uploadDir); err == nil {
		uploadDir = abs
	}

	cfg := &Config{
		HTTPAddr:          getenv("GO_HTTP_ADDR", ":8080"),
		MySQLDSN:          getenv("MYSQL_DSN", "root:123456@tcp(127.0.0.1:3306)/hr_agent?parseTime=true&charset=utf8mb4"),
		UploadDir:         uploadDir,
		AgentBaseURL:      getenv("PYTHON_AGENT_URL", "http://127.0.0.1:8000"),
		SMTPHost:          getenv("SMTP_HOST", "localhost"),
		SMTPPort:          getenvInt("SMTP_PORT", 1025),
		SMTPUser:          getenv("SMTP_USER", ""),
		SMTPPass:          getenv("SMTP_PASS", ""),
		SMTPFrom:          getenv("SMTP_FROM", "hr@example.com"),
		MailDryRun:        getenvBool("MAIL_DRY_RUN", true),
		IMAPHost:          getenv("IMAP_HOST", ""),
		IMAPPort:          getenvInt("IMAP_PORT", 993),
		IMAPUser:          getenv("IMAP_USER", ""),
		IMAPPass:          getenv("IMAP_PASS", ""),
		ReplyTimeoutHours:    getenvInt("REPLY_TIMEOUT_HOURS", 48),
		InterviewerPackHours: getenvInt("INTERVIEWER_PACK_HOURS", 24),
		MaxReschedule:        getenvInt("MAX_RESCHEDULE", 2),

		PublicBaseURL:    stringsTrimRightSlash(getenv("PUBLIC_BASE_URL", "http://127.0.0.1:8080")),
		ReplyTokenSecret: getenv("REPLY_TOKEN_SECRET", "dev-reply-secret-change-me"),
		HRAPIToken:       getenv("HR_API_TOKEN", "dev-hr-token-change-me"),
		InternalAPIToken: getenv("INTERNAL_API_TOKEN", "dev-internal-token-change-me"),
		SessionSecret:    getenv("SESSION_SECRET", ""),

		CalendarProvider:        getenv("CALENDAR_PROVIDER", "memory"),
		FeishuAppID:             getenv("FEISHU_APP_ID", ""),
		FeishuAppSecret:         getenv("FEISHU_APP_SECRET", ""),
		FeishuCalendarID:        getenv("FEISHU_CALENDAR_ID", ""),
		FeishuUserIDType:        getenv("FEISHU_USER_ID_TYPE", "open_id"),
		FeishuTimezone:          getenv("FEISHU_TIMEZONE", "Asia/Shanghai"),
		FeishuLocation:          getenv("FEISHU_LOCATION", "飞书会议 / 线上面试"),

		FeishuLoginEnabled:  getenvBool("FEISHU_LOGIN_ENABLED", true),
		FeishuOAuthRedirect: getenv("FEISHU_OAUTH_REDIRECT_URI", ""),
		FeishuHRAllowOpenIDs: splitCSV(getenv("FEISHU_HR_ALLOW_OPEN_IDS", "")),
		FeishuHRAllowEmails:  splitCSV(getenv("FEISHU_HR_ALLOW_EMAILS", "")),
		AlertWebhookURL:      getenv("ALERT_WEBHOOK_URL", ""),
		ImportConcurrency:    getenvInt("IMPORT_CONCURRENCY", 2),

		ContactRequireResolved:          getenvBool("CONTACT_REQUIRE_RESOLVED", true),
		ContactAllowPlaceholderInDryRun: getenvBool("CONTACT_ALLOW_PLACEHOLDER_IN_DRY_RUN", false),
		ContactPlaceholderDomains:       splitCSV(getenv("CONTACT_PLACEHOLDER_DOMAINS", "import.local")),
		ContactBlocklistDomains:         splitCSV(getenv("CONTACT_BLOCKLIST_DOMAINS", "example.com,test.com,localhost")),
		ImportPreExtract:                getenvBool("IMPORT_PRE_EXTRACT", true),
		ImportDedup:                     strings.ToLower(getenv("IMPORT_DEDUP", "skip")),

		SchedulingAgentEnabled: getenvBool("SCHEDULING_AGENT_ENABLED", true),
	}
	if cfg.SessionSecret == "" {
		cfg.SessionSecret = cfg.ReplyTokenSecret
	}
	if cfg.FeishuOAuthRedirect == "" {
		cfg.FeishuOAuthRedirect = cfg.PublicBaseURL + "/v1/auth/feishu/callback"
	}
	return cfg, nil
}

// loadDotEnv finds repo .env from cwd and next to the binary (Overload: later files win).
func loadDotEnv() {
	seen := map[string]struct{}{}
	try := func(p string) {
		if p == "" {
			return
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		if _, err := os.Stat(p); err == nil {
			_ = overloadEnvFile(p)
		}
	}
	for _, start := range envSearchRoots() {
		dir := start
		for i := 0; i < 8; i++ {
			try(filepath.Join(dir, ".env"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
}

func envSearchRoots() []string {
	var roots []string
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(exe))
	}
	return roots
}

// overloadEnvFile loads .env and overrides process env (handles UTF-8 BOM from Windows editors).
func overloadEnvFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	envMap, err := godotenv.Parse(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	for k, v := range envMap {
		_ = os.Setenv(k, v)
	}
	return nil
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenvBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
}

func (c *Config) ReplyTimeout() time.Duration {
	return time.Duration(c.ReplyTimeoutHours) * time.Hour
}

func (c *Config) InterviewerPackTimeout() time.Duration {
	h := c.InterviewerPackHours
	if h <= 0 {
		h = 24
	}
	return time.Duration(h) * time.Hour
}

func stringsTrimRightSlash(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '/' || s[len(s)-1] == '\\') {
		s = s[:len(s)-1]
	}
	return s
}
