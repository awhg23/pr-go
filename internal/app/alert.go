package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

func (s *Server) sendAlert(ctx context.Context, event string, detail map[string]any) {
	if s.cfg.AlertWebhook == "" {
		return
	}
	payload := map[string]any{
		"event":  event,
		"detail": detail,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		s.logger.Printf("encode alert failed event=%s: %v", event, err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.AlertWebhook, bytes.NewReader(raw))
	if err != nil {
		s.logger.Printf("create alert request failed event=%s: %v", event, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		s.logger.Printf("send alert failed event=%s: %v", event, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.logger.Printf("send alert returned status=%s event=%s", resp.Status, event)
	}
}
