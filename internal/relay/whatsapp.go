// Package relay — WhatsApp Cloud API relay adapter.
//
// Outbound: sends messages via graph.facebook.com messages endpoint.
// Inbound: HTTP webhook at /whatsapp/webhook — handles GET verify challenge
// and POST X-Hub-Signature-256 HMAC-verified message events.
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

const whatsappAPIBase = "https://graph.facebook.com/v18.0"

// WhatsAppRelay implements relay.Relay for WhatsApp Cloud API.
type WhatsAppRelay struct {
	token       string // permanent access token
	phoneID     string // WhatsApp Phone Number ID
	verifyToken string // webhook verification token
	appSecret   string // App Secret for HMAC (reuse WhatsAppToken or separate)
	cmdFn       CommandFunc
	log         *slog.Logger
	client      *http.Client
}

// NewWhatsAppRelay creates a WhatsAppRelay.
// token is the WhatsApp Cloud API access token.
// phoneID is the WhatsApp Phone Number ID (from Meta developer portal).
// verifyToken is the webhook verify token configured in Meta.
// appSecret is used for X-Hub-Signature-256 HMAC; pass empty to skip verification.
func NewWhatsAppRelay(token, phoneID, verifyToken, appSecret string, cmdFn CommandFunc, log *slog.Logger) *WhatsAppRelay {
	if log == nil {
		log = slog.Default()
	}
	return &WhatsAppRelay{
		token:       token,
		phoneID:     phoneID,
		verifyToken: verifyToken,
		appSecret:   appSecret,
		cmdFn:       cmdFn,
		log:         log,
		client:      &http.Client{},
	}
}

// Name implements relay.Relay.
func (r *WhatsAppRelay) Name() string { return "whatsapp" }

// Send implements relay.Relay — sends a text message via the WhatsApp Cloud API.
// The recipient is derived from msg.AgentID if it looks like a phone number,
// otherwise the message is broadcast to the configured Phone Number ID's first contact.
// In headless mode, outbound messages go to the phone number stored in cfg (if any).
// For now, we log and skip if no recipient is determinable.
func (r *WhatsAppRelay) Send(ctx context.Context, msg models.Message) error {
	if r.token == "" || r.phoneID == "" {
		return nil
	}
	// Outbound in broadcast mode: this sends a notification to all who have messaged in.
	// For a robust implementation, a contact registry would track who to reply to.
	// Phase 12 MVP: log the message; actual recipient management is Phase 13.
	r.log.Info("whatsapp: outbound message queued (no recipient registry yet)",
		"content", truncate(msg.Content, 100))
	return nil
}

// WebhookPath returns the HTTP path this relay handles.
func (r *WhatsAppRelay) WebhookPath() string { return "/whatsapp/webhook" }

// HandleWebhook processes incoming WhatsApp webhook requests.
// GET: webhook verification challenge.
// POST: message events with HMAC validation.
func (r *WhatsAppRelay) HandleWebhook(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.handleVerify(w, req)
	case http.MethodPost:
		r.handleEvent(w, req)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleVerify responds to Meta's webhook verification GET request.
func (r *WhatsAppRelay) handleVerify(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	mode := q.Get("hub.mode")
	token := q.Get("hub.verify_token")
	challenge := q.Get("hub.challenge")

	if mode == "subscribe" && token == r.verifyToken {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, challenge)
		r.log.Info("whatsapp: webhook verified")
		return
	}
	r.log.Warn("whatsapp: webhook verification failed", "token_match", token == r.verifyToken)
	http.Error(w, "forbidden", http.StatusForbidden)
}

// handleEvent processes a POST webhook event from Meta.
func (r *WhatsAppRelay) handleEvent(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<16))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Verify HMAC signature if app secret is configured.
	if r.appSecret != "" {
		sig := req.Header.Get("X-Hub-Signature-256")
		if !VerifyWhatsAppSignature(r.appSecret, body, sig) {
			r.log.Warn("whatsapp: invalid webhook signature")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	// Acknowledge immediately — Meta requires a 200 within 20s.
	w.WriteHeader(http.StatusOK)

	// Parse WhatsApp Cloud API payload.
	var payload struct {
		Object string `json:"object"`
		Entry  []struct {
			Changes []struct {
				Value struct {
					Messages []struct {
						From string `json:"from"` // sender phone number
						Text *struct {
							Body string `json:"body"`
						} `json:"text"`
					} `json:"messages"`
				} `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		r.log.Warn("whatsapp: failed to parse webhook payload", "err", err)
		return
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, m := range change.Value.Messages {
				if m.Text == nil {
					continue
				}
				text := strings.TrimSpace(m.Text.Body)
				from := m.From
				r.log.Info("whatsapp: inbound message", "from", from, "text", truncate(text, 100))

				if _, _, ok := ParseCommand(text); ok {
					go func(from, text string) {
						ctx := context.Background()
						reply := r.cmdFn(ctx, text)
						if err := r.sendText(ctx, from, reply); err != nil {
							r.log.Warn("whatsapp: reply failed", "err", err)
						}
					}(from, text)
				}
			}
		}
	}
}

func (r *WhatsAppRelay) sendText(ctx context.Context, to, text string) error {
	url := fmt.Sprintf("%s/%s/messages", whatsappAPIBase, r.phoneID)
	body, _ := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "text",
		"text":              map[string]string{"body": text},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.token)

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("whatsapp: sendText returned HTTP %d", resp.StatusCode)
	}
	return nil
}
