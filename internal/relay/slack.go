// Package relay — Slack relay adapter.
//
// Outbound: chat.postMessage to configured channel.
// Inbound: HTTP webhook endpoint at /slack/events — validates X-Slack-Signature,
// handles url_verification challenge and message events containing slash commands.
// No external SDK — pure net/http.
package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/haepapa/kotui/pkg/models"
)

const slackAPIBase = "https://slack.com/api"

// SlackRelay implements relay.Relay for Slack.
type SlackRelay struct {
	botToken      string
	channelID     string
	signingSecret string
	cmdFn         CommandFunc
	log           *slog.Logger
	client        *http.Client
}

// NewSlackRelay creates a SlackRelay.
func NewSlackRelay(botToken, channelID, signingSecret string, cmdFn CommandFunc, log *slog.Logger) *SlackRelay {
	if log == nil {
		log = slog.Default()
	}
	return &SlackRelay{
		botToken:      botToken,
		channelID:     channelID,
		signingSecret: signingSecret,
		cmdFn:         cmdFn,
		log:           log,
		client:        &http.Client{},
	}
}

// Name implements relay.Relay.
func (r *SlackRelay) Name() string { return "slack" }

// Send implements relay.Relay — posts a message to the configured Slack channel.
func (r *SlackRelay) Send(ctx context.Context, msg models.Message) error {
	if r.botToken == "" || r.channelID == "" {
		return nil
	}
	text := formatMessage(msg)
	return r.postMessage(ctx, r.channelID, text)
}

// WebhookPath returns the HTTP path this relay handles.
func (r *SlackRelay) WebhookPath() string { return "/slack/events" }

// HandleWebhook processes an incoming Slack Events API request.
// It verifies the HMAC signature and dispatches commands.
func (r *SlackRelay) HandleWebhook(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<16))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Verify HMAC signature if signing secret is configured.
	if r.signingSecret != "" {
		ts := req.Header.Get("X-Slack-Request-Timestamp")
		sig := req.Header.Get("X-Slack-Signature")
		if !VerifySlackSignature(r.signingSecret, "v0", ts, body, sig) {
			r.log.Warn("slack: invalid webhook signature")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	// Decode payload.
	var payload struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Event     *struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Channel string `json:"channel"`
			BotID   string `json:"bot_id"` // ignore bot's own messages
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	// url_verification handshake.
	if payload.Type == "url_verification" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"challenge": payload.Challenge})
		return
	}

	// Process message events.
	if payload.Event != nil && payload.Event.Type == "message" && payload.Event.BotID == "" {
		text := strings.TrimSpace(payload.Event.Text)
		channel := payload.Event.Channel
		if _, _, ok := ParseCommand(text); ok {
			go func() {
				ctx := req.Context()
				reply := r.cmdFn(ctx, text)
				if err := r.postMessage(ctx, channel, reply); err != nil {
					r.log.Warn("slack: reply failed", "err", err)
				}
			}()
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (r *SlackRelay) postMessage(ctx context.Context, channel, text string) error {
	body, _ := json.Marshal(map[string]string{
		"channel": channel,
		"text":    text,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		slackAPIBase+"/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.botToken)

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack: postMessage returned HTTP %d", resp.StatusCode)
	}
	return nil
}
