package main

import (
	"context"
	"log"
	"time"

	"github.com/hr-agent/services/internal/config"
	"github.com/hr-agent/services/internal/db"
	"github.com/hr-agent/services/internal/mail"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	sqlDB, err := db.Open(cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("mysql: %v", err)
	}
	defer sqlDB.Close()

	sender := mail.NewQueueSender(sqlDB, mail.SMTPConfig{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort, User: cfg.SMTPUser,
		Pass: cfg.SMTPPass, From: cfg.SMTPFrom, DryRun: cfg.MailDryRun,
	}, 8)

	log.Printf("mailer worker started (dry_run=%v)", cfg.MailDryRun)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		n, err := sender.ProcessOnce(ctx, 20)
		cancel()
		if err != nil {
			log.Printf("mailer error: %v", err)
		} else if n > 0 {
			log.Printf("mailer sent %d", n)
		}
		time.Sleep(2 * time.Second)
	}
}
