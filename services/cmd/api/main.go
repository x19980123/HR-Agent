package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/hr-agent/services/internal/agentclient"
	"github.com/hr-agent/services/internal/api"
	"github.com/hr-agent/services/internal/audit"
	"github.com/hr-agent/services/internal/calendar"
	"github.com/hr-agent/services/internal/config"
	"github.com/hr-agent/services/internal/db"
	"github.com/hr-agent/services/internal/mail"
	"github.com/hr-agent/services/internal/pipeline"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	_ = os.MkdirAll(cfg.UploadDir, 0o755)

	sqlDB, err := db.Open(cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("mysql: %v", err)
	}
	defer sqlDB.Close()

	mailer := mail.NewQueueSender(sqlDB, mail.SMTPConfig{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort, User: cfg.SMTPUser,
		Pass: cfg.SMTPPass, From: cfg.SMTPFrom, DryRun: cfg.MailDryRun,
	}, 4)

	cal, err := calendar.NewFromConfig(cfg)
	if err != nil {
		log.Fatalf("calendar: %v", err)
	}
	log.Printf("calendar provider: %s", cfg.CalendarProvider)

	svc := &pipeline.Service{
		DB:       sqlDB,
		Cfg:      cfg,
		Agent:    agentclient.New(cfg.AgentBaseURL),
		Calendar: cal,
		Mail:     mailer,
		Audit:    &audit.Logger{DB: sqlDB},
	}
	if err := svc.BootstrapSeedAdmin(context.Background()); err != nil {
		log.Printf("staff bootstrap: %v", err)
	} else {
		log.Printf("staff bootstrap: seed admin ready")
	}

	srv := api.NewServer(svc, cfg, cfg.UploadDir)
	log.Printf("go api listening on %s (admin=/admin candidate=/r/{token})", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
