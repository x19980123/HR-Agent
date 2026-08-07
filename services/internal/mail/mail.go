package mail

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hr-agent/services/internal/pii"
)

type Message struct {
	ID             string
	ApplicationID  string
	IdempotencyKey string
	To             string
	Subject        string
	Body           string
}

type Sender interface {
	Enqueue(ctx context.Context, msg Message) error
	ProcessOnce(ctx context.Context, limit int) (int, error)
}

type SMTPConfig struct {
	Host   string
	Port   int
	User   string
	Pass   string
	From   string
	DryRun bool
}

type QueueSender struct {
	DB     *sql.DB
	SMTP   SMTPConfig
	sem    chan struct{}
	mu     sync.Mutex
}

func NewQueueSender(db *sql.DB, smtpCfg SMTPConfig, concurrency int) *QueueSender {
	if concurrency <= 0 {
		concurrency = 4
	}
	return &QueueSender{
		DB:   db,
		SMTP: smtpCfg,
		sem:  make(chan struct{}, concurrency),
	}
}

func (q *QueueSender) Enqueue(ctx context.Context, msg Message) error {
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}
	_, err := q.DB.ExecContext(ctx,
		`INSERT INTO email_outbox (id, application_id, idempotency_key, to_addr, subject, body, status)
		 VALUES (?, ?, ?, ?, ?, ?, 'pending')
		 ON DUPLICATE KEY UPDATE id=id`,
		msg.ID, msg.ApplicationID, msg.IdempotencyKey, msg.To, msg.Subject, msg.Body,
	)
	return err
}

func (q *QueueSender) ProcessOnce(ctx context.Context, limit int) (int, error) {
	rows, err := q.DB.QueryContext(ctx,
		`SELECT id, application_id, idempotency_key, to_addr, subject, body, attempts
		 FROM email_outbox WHERE status='pending' ORDER BY created_at ASC LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type row struct {
		id, app, idem, to, subject, body string
		attempts                         int
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.app, &r.idem, &r.to, &r.subject, &r.body, &r.attempts); err != nil {
			return 0, err
		}
		list = append(list, r)
	}

	var wg sync.WaitGroup
	var sent int
	var mu sync.Mutex
	for _, r := range list {
		r := r
		wg.Add(1)
		q.sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-q.sem }()
			if err := q.send(r.to, r.subject, r.body); err != nil {
				_, _ = q.DB.ExecContext(ctx,
					`UPDATE email_outbox SET attempts=attempts+1, last_error=?, status=IF(attempts+1>=5,'failed','pending') WHERE id=?`,
					err.Error(), r.id)
				log.Printf("mail send fail app=%s err=%v", r.app, err)
				return
			}
			_, _ = q.DB.ExecContext(ctx,
				`UPDATE email_outbox SET status='sent', sent_at=?, attempts=attempts+1 WHERE id=?`,
				time.Now(), r.id)
			mu.Lock()
			sent++
			mu.Unlock()
			log.Printf("mail sent app=%s to=%s subject=%s", r.app, pii.Redact(r.to), r.subject)
		}()
	}
	wg.Wait()
	return sent, nil
}

func (q *QueueSender) send(to, subject, body string) error {
	if q.SMTP.DryRun {
		log.Printf("[DRY-RUN MAIL] to=%s subject=%s\n%s", pii.Redact(to), subject, pii.Redact(body))
		return nil
	}
	msg := buildUTF8PlainMessage(q.SMTP.From, to, subject, body)
	var auth smtp.Auth
	if q.SMTP.User != "" {
		auth = smtp.PlainAuth("", q.SMTP.User, q.SMTP.Pass, q.SMTP.Host)
	}
	addr := fmt.Sprintf("%s:%d", q.SMTP.Host, q.SMTP.Port)
	if q.SMTP.Port == 465 {
		return sendMailTLS(addr, q.SMTP.Host, auth, q.SMTP.From, []string{to}, msg)
	}
	return smtp.SendMail(addr, auth, q.SMTP.From, []string{to}, msg)
}

// buildUTF8PlainMessage encodes Subject/body for UTF-8 (fixes QQ→Outlook mojibake).
func buildUTF8PlainMessage(from, to, subject, body string) []byte {
	encSubject := mime.BEncoding.Encode("utf-8", subject)
	bodyB64 := base64.StdEncoding.EncodeToString([]byte(body))
	var folded strings.Builder
	for i := 0; i < len(bodyB64); i += 76 {
		end := i + 76
		if end > len(bodyB64) {
			end = len(bodyB64)
		}
		folded.WriteString(bodyB64[i:end])
		folded.WriteString("\r\n")
	}
	text := strings.TrimSuffix(folded.String(), "\r\n")
	return []byte(strings.Join([]string{
		"From: " + from,
		"To: " + to,
		"Subject: " + encSubject,
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: base64",
		"",
		text,
	}, "\r\n"))
}

func sendMailTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	tlsCfg := &tls.Config{ServerName: host}
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
