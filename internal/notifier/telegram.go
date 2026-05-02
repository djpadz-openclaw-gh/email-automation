package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// Telegram sends notifications via the Telegram Bot API.
type Telegram struct {
	botToken string
	chatID   string
	client   *http.Client
}

// NewTelegram creates a new Telegram notifier.
func NewTelegram(botToken, chatID string) *Telegram {
	return &Telegram{
		botToken: botToken,
		chatID:   chatID,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Enabled returns true if Telegram is configured.
func (t *Telegram) Enabled() bool {
	return t.botToken != "" && t.chatID != ""
}

// SendMessage sends a text message via Telegram.
func (t *Telegram) SendMessage(ctx context.Context, text string) error {
	if !t.Enabled() {
		log.Warn().Msg("telegram not configured, skipping notification")
		return nil
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)

	payload := map[string]interface{}{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}

	return nil
}

// NotifyRuleAction sends a formatted notification about a rule action.
func (t *Telegram) NotifyRuleAction(ctx context.Context, ruleName, action, subject, sender, target string) error {
	var emoji string
	switch action {
	case "delete":
		emoji = "🗑"
	case "move":
		emoji = "📁"
	case "archive":
		emoji = "📦"
	case "keep":
		emoji = "📌"
	default:
		emoji = "📧"
	}

	text := fmt.Sprintf(
		"%s <b>%s</b>\n\n"+
			"<b>Rule:</b> %s\n"+
			"<b>Action:</b> %s\n"+
			"<b>From:</b> %s\n"+
			"<b>Subject:</b> %s",
		emoji, action, ruleName, action, sender, subject,
	)

	if target != "" {
		text += fmt.Sprintf("\n<b>Target:</b> %s", target)
	}

	return t.SendMessage(ctx, text)
}
