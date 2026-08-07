package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hr-agent/services/internal/config"
	"github.com/hr-agent/services/internal/db"
	"github.com/hr-agent/services/internal/pipeline"
	"github.com/hr-agent/services/internal/agentclient"
	"github.com/hr-agent/services/internal/audit"
	"github.com/hr-agent/services/internal/calendar"
	"github.com/hr-agent/services/internal/mail"
)

// ingress supports:
// 1) webhook HTTP server POST /v1/hooks/email-reply
// 2) periodic timeout sweep
// 3) optional IMAP poll stub via EMAIL_REPLY_FILE for local demo
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	mode := getenv("INGRESS_MODE", "webhook")
	apiBase := getenv("GO_API_BASE", "http://127.0.0.1:8080")

	switch mode {
	case "embedded":
		runEmbedded(cfg)
	default:
		runWebhookProxy(cfg, apiBase)
	}
}

func runWebhookProxy(cfg *config.Config, apiBase string) {
	addr := getenv("INGRESS_HTTP_ADDR", ":8081")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/hooks/email-reply", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			ApplicationID string `json:"application_id"`
			ThreadID      string `json:"thread_id"`
			EmailBody     string `json:"email_body"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		id := payload.ApplicationID
		if id == "" {
			id = "by-thread"
		}
		forward, _ := json.Marshal(map[string]string{
			"email_body": payload.EmailBody,
			"thread_id":  payload.ThreadID,
		})
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
			strings.TrimRight(apiBase, "/")+"/v1/applications/"+id+"/replies", bytes.NewReader(forward))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.InternalAPIToken != "" {
			req.Header.Set("X-Internal-Token", cfg.InternalAPIToken)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})

	// timeout sweeper
	go func() {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(apiBase, "/")+"/v1/internal/sweep-timeouts", nil)
			if cfg.InternalAPIToken != "" {
				req.Header.Set("X-Internal-Token", cfg.InternalAPIToken)
			}
			resp, err := http.DefaultClient.Do(req)
			cancel()
			if err != nil {
				log.Printf("sweep error: %v", err)
			} else {
				resp.Body.Close()
			}
			time.Sleep(5 * time.Minute)
		}
	}()

	// local demo: watch a file drop for simulated IMAP
	go watchReplyFile(apiBase, cfg.InternalAPIToken)

	log.Printf("ingress webhook on %s (api=%s)", addr, apiBase)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func runEmbedded(cfg *config.Config) {
	sqlDB, err := db.Open(cfg.MySQLDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer sqlDB.Close()
	mailer := mail.NewQueueSender(sqlDB, mail.SMTPConfig{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort, From: cfg.SMTPFrom, DryRun: cfg.MailDryRun,
	}, 4)
	svc := &pipeline.Service{
		DB: sqlDB, Cfg: cfg, Agent: agentclient.New(cfg.AgentBaseURL),
		Mail: mailer, Audit: &audit.Logger{DB: sqlDB},
	}
	cal, err := calendar.NewFromConfig(cfg)
	if err != nil {
		log.Fatalf("calendar: %v", err)
	}
	svc.Calendar = cal
	log.Printf("ingress embedded mode: sweeping timeouts (calendar=%s)", cfg.CalendarProvider)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		n, err := svc.SweepTimeouts(ctx)
		cancel()
		if err != nil {
			log.Printf("sweep: %v", err)
		} else if n > 0 {
			log.Printf("timeout -> needs_human: %d", n)
		}
		time.Sleep(5 * time.Minute)
	}
}

func watchReplyFile(apiBase, internalToken string) {
	path := getenv("EMAIL_REPLY_FILE", "")
	if path == "" {
		return
	}
	log.Printf("watching reply file %s", path)
	var lastMod time.Time
	for {
		fi, err := os.Stat(path)
		if err == nil && fi.ModTime().After(lastMod) {
			lastMod = fi.ModTime()
			b, err := os.ReadFile(path)
			if err == nil && len(b) > 0 {
				payload, _ := json.Marshal(map[string]string{"email_body": string(b)})
				req, _ := http.NewRequest(http.MethodPost,
					strings.TrimRight(apiBase, "/")+"/v1/applications/by-thread/replies", bytes.NewReader(payload))
				req.Header.Set("Content-Type", "application/json")
				if internalToken != "" {
					req.Header.Set("X-Internal-Token", internalToken)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					log.Printf("reply file forward err: %v", err)
				} else {
					resp.Body.Close()
					log.Printf("forwarded reply file status=%d", resp.StatusCode)
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
