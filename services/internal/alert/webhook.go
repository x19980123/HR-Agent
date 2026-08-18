package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Notify posts a short text payload to ALERT_WEBHOOK_URL (Feishu/Slack generic).
func Notify(ctx context.Context, webhookURL, title, body string) error {
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		return nil
	}
	payload := map[string]any{
		"msg_type": "text",
		"content": map[string]string{
			"text": title + "\n" + body,
		},
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("alert webhook status %d", resp.StatusCode)
	}
	return nil
}
